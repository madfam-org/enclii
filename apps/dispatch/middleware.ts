import { NextResponse } from 'next/server'
import type { NextRequest } from 'next/server'
import { jwtVerify, createRemoteJWKSet } from 'jose'

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
  return ALLOWED_DOMAINS.some((domain) => email.toLowerCase().endsWith(domain.toLowerCase()))
}

function hasAllowedRole(roles: string[]): boolean {
  return roles.some((role) => ALLOWED_ROLES.includes(role))
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
  return publicPaths.some(
    (path) => pathname === path || pathname.startsWith(path + '/')
  )
}

/**
 * Extract roles from verified JWT payload.
 * Supports both array and comma-separated string formats.
 */
function extractRoles(payload: Record<string, unknown>): string[] {
  const rolesRaw = payload.roles || payload.role || payload['enclii_roles']
  if (Array.isArray(rolesRaw)) {
    return rolesRaw.filter((r): r is string => typeof r === 'string')
  }
  if (typeof rolesRaw === 'string') {
    return rolesRaw.split(',').map((r) => r.trim())
  }
  return []
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

  try {
    const { payload } = await jwtVerify(token, jwks, {
      issuer: JANUA_ISSUER,
    })

    email = payload.email as string | undefined
    roles = extractRoles(payload as Record<string, unknown>)
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

  if (!domainAllowed || !roleAllowed) {
    const reason = !domainAllowed
      ? `email domain not allowed: ${email}`
      : `insufficient role: ${roles.join(',') || 'none'}`
    console.warn(`[DISPATCH SECURITY] Unauthorized access attempt - ${reason}`)

    if (pathname.startsWith('/api/')) {
      return new NextResponse(
        JSON.stringify({
          error: 'Forbidden',
          message: 'Dispatch access is restricted to authorized infrastructure operators.',
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
      "frame-ancestors 'none'",
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
