/**
 * Server-side helpers for API route handlers that need to inspect the
 * authenticated user's roles beyond what the middleware enforces.
 *
 * The middleware already gates every protected route on (allowed domain
 * AND allowed role). API routes use these helpers when they need a
 * STRICTER check — e.g. "Sync Now" requires superadmin specifically,
 * even though admin/operator can view ArgoCD state.
 */
import { jwtVerify, createRemoteJWKSet } from 'jose'
import { extractRoles } from './auth-helpers'
import type { NextRequest } from 'next/server'

const JANUA_ISSUER =
  process.env.JANUA_ISSUER || process.env.NEXT_PUBLIC_JANUA_URL || 'https://auth.madfam.io'
const JWKS_URL = new URL('/.well-known/jwks.json', JANUA_ISSUER)
const jwks = createRemoteJWKSet(JWKS_URL)

export interface AuthClaims {
  email: string | null
  roles: string[]
}

export async function verifyAuth(request: NextRequest): Promise<AuthClaims | null> {
  const token =
    request.cookies.get('dispatch_auth')?.value || request.cookies.get('admin_auth')?.value
  if (!token) return null
  try {
    const { payload } = await jwtVerify(token, jwks, { issuer: JANUA_ISSUER })
    return {
      email: (payload.email as string | undefined) ?? null,
      roles: extractRoles(payload as Record<string, unknown>),
    }
  } catch {
    return null
  }
}

export function hasRole(claims: AuthClaims | null, ...required: string[]): boolean {
  if (!claims) return false
  return claims.roles.some((r) => required.includes(r))
}
