import { NextResponse } from 'next/server'
import type { NextRequest } from 'next/server'
import { jwtVerify, createRemoteJWKSet } from 'jose'
import {
  isAllowedDomain as _isAllowedDomain,
  hasAllowedRole as _hasAllowedRole,
  isPublicPath as _isPublicPath,
  extractRoles as _extractRoles,
  platformRankFromClaims as _platformRankFromClaims,
} from './lib/auth-helpers'

/**
 * Dispatch Middleware - Infrastructure Operator Access Control
 *
 * SECURITY: This middleware enforces strict access control for Dispatch.
 * Only authorized infrastructure operators can access the Control Tower.
 *
 * Authorization is verified from the JWT token (not client-writable cookies):
 * 1. JWT is verified against Janua JWKS endpoint
 * 2. Email domain must be from an allowed domain (configurable via env)
 * 3. User role must be an operator-level role (superadmin, admin, operator)
 * 4. ADR-003: if the token STATES the platform rank, it must not state false
 *
 * ADR-003 AND WHY STEP 4 IS SHAPED THE WAY IT IS
 * ----------------------------------------------
 * Every route this console drives (/v1/admin/*) now requires the
 * `platform_admin` rank at the API. That rank is a column in the API's
 * database, reconciled from an operator allow-list — it is deliberately not a
 * role string, because an API token's scopes are copied into the caller's
 * roles and a rank assertable by string is a rank a tenant administrator can
 * mint for itself. So the role list below CANNOT be made to mean the same
 * thing as the API's gate, and narrowing it to a `platform_admin` role would
 * be theatre: it would deny on a string the API refuses to trust.
 *
 * What the console does instead is the honest half. When the verified token
 * states the rank, a stated `false` is a denial — a principal the API will
 * refuse on every call has no reason to be handed the portal. When the token
 * says nothing (today's Janua tokens do not carry the claim), the legacy role
 * list still admits, and the console logs that it is doing so. It does NOT
 * fail closed on a missing claim: that would lock every operator out of the
 * console the day this deploys, in service of a check that is not the
 * enforcement point anyway.
 *
 * ADR-003 §5 is explicit that the UI is not an enforcement point. This
 * middleware is an affordance: the API refuses the call regardless of what
 * happens here, and nothing here may be relied on for tenant isolation.
 *
 * Configure via environment variables:
 * - JANUA_ISSUER: OIDC issuer URL (e.g., https://auth.madfam.io)
 * - ALLOWED_ADMIN_DOMAINS: Comma-separated list of allowed email domains
 * - ALLOWED_ADMIN_ROLES: Comma-separated list of allowed roles
 */

// Janua OIDC configuration
const JANUA_ISSUER = process.env.JANUA_ISSUER || process.env.NEXT_PUBLIC_JANUA_URL || 'https://auth.madfam.io'
const JWKS_URL = new URL('/.well-known/jwks.json', JANUA_ISSUER)
const jwks = createRemoteJWKSet(JWKS_URL)

// Allowed email domains (configurable via env, fallback to example.org for OSS deployments)
const DEFAULT_DOMAINS = ['@example.org']
const ALLOWED_DOMAINS = process.env.ALLOWED_ADMIN_DOMAINS
  ? process.env.ALLOWED_ADMIN_DOMAINS.split(',').map((d) => d.trim())
  : DEFAULT_DOMAINS

// Allowed roles (configurable via env)
const DEFAULT_ROLES = ['superadmin', 'admin', 'operator']
const ALLOWED_ROLES = process.env.ALLOWED_ADMIN_ROLES
  ? process.env.ALLOWED_ADMIN_ROLES.split(',').map((r) => r.trim())
  : DEFAULT_ROLES

function isAllowedDomain(email: string): boolean {
  return _isAllowedDomain(email, ALLOWED_DOMAINS)
}

function hasAllowedRole(roles: string[]): boolean {
  return _hasAllowedRole(roles, ALLOWED_ROLES)
}

// Public paths that don't require authentication
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

function isPublicPath(pathname: string): boolean {
  return _isPublicPath(pathname, publicPaths)
}

/**
 * Extract roles from verified JWT payload.
 * Supports both array and comma-separated string formats.
 */
function extractRoles(payload: Record<string, unknown>): string[] {
  return _extractRoles(payload)
}

/**
 * Read the ADR-003 platform rank from the verified payload.
 * `null` means the token does not carry the claim at all.
 */
function platformRankFromClaims(payload: Record<string, unknown>): boolean | null {
  return _platformRankFromClaims(payload)
}

export async function middleware(request: NextRequest) {
  const { pathname } = request.nextUrl

  // Skip middleware for public paths
  if (isPublicPath(pathname)) {
    return addSecurityHeaders(NextResponse.next())
  }

  // Extract JWT from auth cookie (set by the auth callback, NOT user-writable role cookies)
  const token = request.cookies.get('dispatch_auth')?.value || request.cookies.get('admin_auth')?.value

  if (!token) {
    if (pathname.startsWith('/api/')) {
      return new NextResponse(JSON.stringify({ error: 'Unauthorized' }), {
        status: 401,
        headers: { 'Content-Type': 'application/json' },
      })
    }
    return NextResponse.redirect(new URL('/login', request.url))
  }

  // Verify the JWT against Janua JWKS — this is the security-critical check.
  // We no longer trust client-writable cookies for email/roles.
  let email: string | undefined
  let roles: string[] = []
  let platformRank: boolean | null = null

  try {
    const { payload } = await jwtVerify(token, jwks, {
      issuer: JANUA_ISSUER,
    })

    email = payload.email as string | undefined
    roles = extractRoles(payload as Record<string, unknown>)
    platformRank = platformRankFromClaims(payload as Record<string, unknown>)
  } catch (err) {
    console.warn(`[DISPATCH SECURITY] JWT verification failed: ${err}`)

    if (pathname.startsWith('/api/')) {
      return new NextResponse(JSON.stringify({ error: 'Invalid or expired token' }), {
        status: 401,
        headers: { 'Content-Type': 'application/json' },
      })
    }
    // Clear stale auth cookie and redirect to login
    const response = NextResponse.redirect(new URL('/login', request.url))
    response.cookies.delete('dispatch_auth')
    response.cookies.delete('admin_auth')
    return response
  }

  // OPERATOR CHECK: Must have allowed domain AND allowed role (from verified JWT)
  const domainAllowed = email ? isAllowedDomain(email) : false
  const roleAllowed = hasAllowedRole(roles)

  // ADR-003: a token that STATES the platform rank as false names a principal
  // the API will refuse on every /v1/admin/* call. A token that says nothing
  // about the rank is not a denial — see the note at the top of this file.
  const rankDenied = platformRank === false
  if (platformRank === null && domainAllowed && roleAllowed) {
    console.warn(
      '[DISPATCH SECURITY] token carries no ADR-003 platform-rank claim; admitting on the legacy role list. ' +
        'The API is the enforcement point and will refuse /v1/admin/* without the platform_admin rank.'
    )
  }

  if (!domainAllowed || !roleAllowed || rankDenied) {
    const reason = !domainAllowed
      ? `email domain not allowed: ${email}`
      : rankDenied
        ? 'token states is_platform_admin=false (ADR-003); tenant administrators are scoped to their own tenant'
        : `insufficient role: ${roles.join(',') || 'none'}`
    console.warn(`[DISPATCH SECURITY] Unauthorized access attempt - ${reason}`)

    if (pathname.startsWith('/api/')) {
      return new NextResponse(
        JSON.stringify({
          error: 'Forbidden',
          message: rankDenied
            ? 'Dispatch requires the platform_admin rank (ADR-003). Tenant administrators are scoped to their own tenant.'
            : 'Dispatch access is restricted to authorized infrastructure operators.',
        }),
        {
          status: 403,
          headers: { 'Content-Type': 'application/json' },
        }
      )
    }

    return NextResponse.redirect(new URL('/access-denied', request.url))
  }

  return addSecurityHeaders(NextResponse.next())
}

function addSecurityHeaders(response: NextResponse): NextResponse {
  const securityHeaders = {
    'X-Frame-Options': 'DENY',
    'X-Content-Type-Options': 'nosniff',
    'X-XSS-Protection': '1; mode=block',
    'Referrer-Policy': 'strict-origin-when-cross-origin',
    'Content-Security-Policy': [
      "default-src 'self'",
      "script-src 'self' 'unsafe-eval' 'unsafe-inline' https://static.cloudflareinsights.com",
      "style-src 'self' 'unsafe-inline' https://fonts.googleapis.com",
      "font-src 'self' data: https://fonts.gstatic.com",
      "img-src 'self' data: https:",
      `connect-src 'self' ${process.env.NODE_ENV !== 'production' ? 'http://localhost:4200 ' : ''}https://api.enclii.dev https://api.cloudflare.com ${process.env.NEXT_PUBLIC_JANUA_URL || 'https://api.janua.dev'}`,
      "frame-ancestors 'self' https://selva.town https://*.selva.town https://*.madfam.io",
    ].join('; '),
    'Permissions-Policy': [
      'geolocation=()',
      'microphone=()',
      'camera=()',
      'payment=()',
      'usb=()',
    ].join(', '),
    ...(process.env.NODE_ENV === 'production' && {
      'Strict-Transport-Security': 'max-age=31536000; includeSubDomains; preload',
    }),
  }

  Object.entries(securityHeaders).forEach(([key, value]) => {
    if (value) {
      response.headers.set(key, value)
    }
  })

  return response
}

export const config = {
  matcher: ['/((?!_next/static|_next/image|favicon.ico|public).*)'],
}
