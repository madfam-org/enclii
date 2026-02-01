import { NextResponse } from 'next/server'
import type { NextRequest } from 'next/server'

/**
 * Dispatch Middleware - Infrastructure Operator Access Control
 *
 * SECURITY: This middleware enforces strict access control for Dispatch.
 * Only authorized infrastructure operators can access the Control Tower.
 *
 * Authorization is based on:
 * 1. Email domain - must be from an allowed domain (configurable via env)
 * 2. User role - must have an operator-level role (superadmin, admin, operator)
 *
 * Configure via environment variables:
 * - ALLOWED_ADMIN_DOMAINS: Comma-separated list of allowed email domains (e.g., @yourcompany.com)
 * - ALLOWED_ADMIN_ROLES: Comma-separated list of allowed roles (default: superadmin,admin,operator)
 */

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

/**
 * Check if an email is from an allowed domain
 */
function isAllowedDomain(email: string): boolean {
  return ALLOWED_DOMAINS.some((domain) => email.toLowerCase().endsWith(domain.toLowerCase()))
}

/**
 * Check if user has an allowed role
 * Roles are stored as comma-separated string in cookie
 */
function hasAllowedRole(rolesString: string | undefined): boolean {
  if (!rolesString) return false
  const userRoles = rolesString.split(',').map((r) => r.trim())
  return userRoles.some((role) => ALLOWED_ROLES.includes(role))
}

// Public paths that don't require authentication
const publicPaths = [
  '/login',
  '/auth/callback',
  '/api/auth', // Includes /api/auth/session for cookie setting
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

export function middleware(request: NextRequest) {
  const { pathname } = request.nextUrl

  // Skip middleware for public paths
  if (isPublicPath(pathname)) {
    return addSecurityHeaders(NextResponse.next())
  }

  // Check for authentication token and user info
  const token = request.cookies.get('dispatch_auth')?.value || request.cookies.get('admin_auth')?.value
  const userEmail = request.cookies.get('dispatch_user_email')?.value || request.cookies.get('admin_user_email')?.value
  const userRoles = request.cookies.get('dispatch_user_roles')?.value || request.cookies.get('admin_user_roles')?.value

  // If no token, redirect to login
  if (!token) {
    if (pathname.startsWith('/api/')) {
      return new NextResponse(JSON.stringify({ error: 'Unauthorized' }), {
        status: 401,
        headers: { 'Content-Type': 'application/json' },
      })
    }
    return NextResponse.redirect(new URL('/login', request.url))
  }

  // OPERATOR CHECK: Must have allowed domain AND allowed role
  const domainAllowed = userEmail ? isAllowedDomain(userEmail) : false
  const roleAllowed = hasAllowedRole(userRoles)

  if (!domainAllowed || !roleAllowed) {
    const reason = !domainAllowed
      ? `email domain not allowed: ${userEmail}`
      : `insufficient role: ${userRoles || 'none'}`
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

    // Redirect unauthorized users to access denied page
    return NextResponse.redirect(new URL('/access-denied', request.url))
  }

  return addSecurityHeaders(NextResponse.next())
}

function addSecurityHeaders(response: NextResponse): NextResponse {
  const securityHeaders = {
    // Prevent clickjacking attacks
    'X-Frame-Options': 'DENY',

    // Prevent MIME type sniffing
    'X-Content-Type-Options': 'nosniff',

    // Enable XSS protection
    'X-XSS-Protection': '1; mode=block',

    // Control referrer information
    'Referrer-Policy': 'strict-origin-when-cross-origin',

    // Strict Content Security Policy for Dispatch
    'Content-Security-Policy': [
      "default-src 'self'",
      "script-src 'self' 'unsafe-eval' 'unsafe-inline' https://static.cloudflareinsights.com",
      "style-src 'self' 'unsafe-inline' https://fonts.googleapis.com",
      "font-src 'self' data: https://fonts.gstatic.com",
      "img-src 'self' data: https:",
      `connect-src 'self' ${process.env.NODE_ENV !== 'production' ? 'http://localhost:4200 ' : ''}https://api.enclii.dev https://api.cloudflare.com ${process.env.NEXT_PUBLIC_JANUA_URL || 'https://api.janua.dev'}`,
      "frame-ancestors 'none'",
    ].join('; '),

    // Restrict browser features
    'Permissions-Policy': [
      'geolocation=()',
      'microphone=()',
      'camera=()',
      'payment=()',
      'usb=()',
    ].join(', '),

    // HSTS in production
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
