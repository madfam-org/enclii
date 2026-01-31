'use client'

import { createContext, useContext, useEffect, useState, useCallback, ReactNode } from 'react'
import { useRouter } from 'next/navigation'

/**
 * AuthContext for Dispatch
 *
 * Handles Janua SSO authentication with infrastructure operator validation.
 * Uses PKCE (Proof Key for Code Exchange) for secure OAuth 2.0 flow.
 * Access is restricted based on:
 * 1. Email domain - must be from an allowed domain (configurable via env)
 * 2. User role - must have an operator-level role (superadmin, admin, operator)
 */

const JANUA_URL = process.env.NEXT_PUBLIC_JANUA_URL || 'https://auth.madfam.io'
const OAUTH_CLIENT_ID = process.env.NEXT_PUBLIC_OAUTH_CLIENT_ID || 'jnc_lofqyf9LQXG_OwENAIw89p_XvngkWMi-'

// PKCE helpers for secure OAuth 2.0 flow
function generateCodeVerifier(): string {
  const array = new Uint8Array(32)
  crypto.getRandomValues(array)
  return Array.from(array, (byte) => byte.toString(16).padStart(2, '0')).join('')
}

async function generateCodeChallenge(verifier: string): Promise<string> {
  const encoder = new TextEncoder()
  const data = encoder.encode(verifier)
  const digest = await crypto.subtle.digest('SHA-256', data)
  const base64 = btoa(String.fromCharCode(...new Uint8Array(digest)))
  // Base64URL encoding (replace + with -, / with _, remove =)
  return base64.replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '')
}

// Allowed email domains (must match middleware configuration, fallback to example.org for OSS)
const DEFAULT_DOMAINS = ['@example.org']
const ALLOWED_DOMAINS = process.env.NEXT_PUBLIC_ALLOWED_ADMIN_DOMAINS
  ? process.env.NEXT_PUBLIC_ALLOWED_ADMIN_DOMAINS.split(',').map((d) => d.trim())
  : DEFAULT_DOMAINS

// Allowed roles (must match middleware configuration)
const DEFAULT_ROLES = ['superadmin', 'admin', 'operator']
const ALLOWED_ROLES = process.env.NEXT_PUBLIC_ALLOWED_ADMIN_ROLES
  ? process.env.NEXT_PUBLIC_ALLOWED_ADMIN_ROLES.split(',').map((r) => r.trim())
  : DEFAULT_ROLES

/**
 * Check if an email is from an allowed domain
 */
function isAllowedDomain(email: string): boolean {
  return ALLOWED_DOMAINS.some((domain) => email.toLowerCase().endsWith(domain.toLowerCase()))
}

/**
 * Check if user has an allowed role
 */
function hasAllowedRole(roles: string[] | undefined): boolean {
  if (!roles || roles.length === 0) return false
  return roles.some((role) => ALLOWED_ROLES.includes(role))
}

interface User {
  id: string
  email: string
  name?: string
  is_admin: boolean
  roles?: string[]
}

interface AuthContextType {
  user: User | null
  isLoading: boolean
  isAuthenticated: boolean
  isAuthorized: boolean
  login: () => void
  logout: () => void
  error: string | null
}

const AuthContext = createContext<AuthContextType | undefined>(undefined)

export function AuthProvider({ children }: { children: ReactNode }) {
  const [user, setUser] = useState<User | null>(null)
  const [isLoading, setIsLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const router = useRouter()

  // User is authorized if they have both allowed domain AND allowed role
  const isAuthorized =
    user !== null && isAllowedDomain(user.email) && hasAllowedRole(user.roles)

  // Check authentication status on mount
  useEffect(() => {
    checkAuth()
  }, [])

  const checkAuth = useCallback(async () => {
    try {
      const token = document.cookie.split('; ').find(r => r.startsWith('dispatch_auth='))?.split('=')[1]
      if (!token) {
        setIsLoading(false)
        return
      }

      // Verify token with Janua
      const response = await fetch(`${JANUA_URL}/api/v1/auth/me`, {
        headers: {
          Authorization: `Bearer ${token}`,
        },
      })

      if (response.ok) {
        const userData = await response.json()

        // Extract roles from user data (Janua returns roles as array)
        const userRoles: string[] = userData.roles || []
        // Also check is_admin flag for backwards compatibility
        if (userData.is_admin && !userRoles.includes('admin')) {
          userRoles.push('admin')
        }

        // SECURITY: Validate domain and role
        const domainOk = isAllowedDomain(userData.email)
        const roleOk = hasAllowedRole(userRoles)

        // Build cookie options with proper domain for cross-subdomain support
        const hostname = typeof window !== 'undefined' ? window.location.hostname : ''
        const cookieDomain = hostname.includes('.enclii.dev') ? '; domain=.enclii.dev' : ''
        const secure = typeof window !== 'undefined' && window.location.protocol === 'https:' ? '; Secure' : ''
        const cookieBase = `; path=/; max-age=86400; SameSite=Lax${cookieDomain}${secure}`
        const cookieClear = `; Max-Age=0; path=/${cookieDomain}`

        if (!domainOk || !roleOk) {
          const reason = !domainOk
            ? 'Your email domain is not authorized for Dispatch access.'
            : 'You do not have the required role for Dispatch access.'
          setError(`Access denied. ${reason}`)
          document.cookie = `dispatch_auth=${cookieClear}`
          document.cookie = `dispatch_user_email=${cookieClear}`
          document.cookie = `dispatch_user_roles=${cookieClear}`
          setUser(null)
        } else {
          // Set user with roles
          setUser({ ...userData, roles: userRoles })
          // Set cookies for middleware (roles as comma-separated string)
          document.cookie = `dispatch_auth=${token}${cookieBase}`
          document.cookie = `dispatch_user_email=${userData.email}${cookieBase}`
          document.cookie = `dispatch_user_roles=${userRoles.join(',')}${cookieBase}`
        }
      } else {
        const elseHostname = typeof window !== 'undefined' ? window.location.hostname : ''
        const clearDomain = elseHostname.includes('.enclii.dev') ? '; domain=.enclii.dev' : ''
        document.cookie = `dispatch_auth=; Max-Age=0; path=/${clearDomain}`
        document.cookie = `dispatch_user_email=; Max-Age=0; path=/${clearDomain}`
        document.cookie = `dispatch_user_roles=; Max-Age=0; path=/${clearDomain}`
      }
    } catch (err) {
      console.error('Auth check failed:', err)
      setError('Authentication failed')
    } finally {
      setIsLoading(false)
    }
  }, [])

  const login = useCallback(async () => {
    // Generate PKCE parameters for secure OAuth 2.0 flow
    const codeVerifier = generateCodeVerifier()
    const codeChallenge = await generateCodeChallenge(codeVerifier)

    // Store code_verifier for the callback (will be used in token exchange)
    sessionStorage.setItem('dispatch_code_verifier', codeVerifier)

    // Redirect to Janua OAuth authorize endpoint with PKCE
    const params = new URLSearchParams({
      response_type: 'code',
      client_id: OAUTH_CLIENT_ID,
      redirect_uri: `${window.location.origin}/auth/callback`,
      scope: 'openid profile email',
      code_challenge: codeChallenge,
      code_challenge_method: 'S256',
    })
    window.location.href = `${JANUA_URL}/api/v1/oauth/authorize?${params.toString()}`
  }, [])

  const logout = useCallback(async () => {
    try {
      const token = document.cookie.split('; ').find(r => r.startsWith('dispatch_auth='))?.split('=')[1]
      if (token) {
        // Notify Janua of logout
        await fetch(`${JANUA_URL}/api/v1/auth/logout`, {
          method: 'POST',
          headers: {
            Authorization: `Bearer ${token}`,
          },
        }).catch(() => {})
      }
    } finally {
      // Clear cookies with proper domain
      const hostname = typeof window !== 'undefined' ? window.location.hostname : ''
      const clearDomain = hostname.includes('.enclii.dev') ? '; domain=.enclii.dev' : ''
      document.cookie = `dispatch_auth=; Max-Age=0; path=/${clearDomain}`
      document.cookie = `dispatch_user_email=; Max-Age=0; path=/${clearDomain}`
      document.cookie = `dispatch_user_roles=; Max-Age=0; path=/${clearDomain}`
      setUser(null)
      router.push('/login')
    }
  }, [router])

  return (
    <AuthContext.Provider
      value={{
        user,
        isLoading,
        isAuthenticated: !!user,
        isAuthorized,
        login,
        logout,
        error,
      }}
    >
      {children}
    </AuthContext.Provider>
  )
}

export function useAuth() {
  const context = useContext(AuthContext)
  if (context === undefined) {
    throw new Error('useAuth must be used within an AuthProvider')
  }
  return context
}
