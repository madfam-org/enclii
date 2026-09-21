'use client'

import { createContext, useContext, useEffect, useState, useCallback, ReactNode } from 'react'
import { useRouter } from 'next/navigation'
import { adoptSidFromLocation, withTabSessionHeader } from '@/lib/tab-session'

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

/**
 * Janua OIDC RP-Initiated Logout (end_session) endpoint path.
 *
 * Confirmed against the janua repo (apps/api/app/routers/v1/oauth_provider.py,
 * `logout_router` mounted WITHOUT the `/api/v1` prefix — see
 * `app.include_router(oauth_provider_v1.logout_router)` in main.py). The route
 * is therefore served at the API root as `GET /logout`, NOT
 * `/api/v1/auth/logout`. It requires `client_id` and `post_logout_redirect_uri`
 * query params (and an optional `state`), clears the `janua_sso` cookie, revokes
 * the referenced session row, then 302s to the post-logout URI.
 *
 * Kept as a single constant so it is trivial to update if the janua lane lands
 * the endpoint at a different path before it deploys.
 */
const JANUA_LOGOUT_PATH = process.env.NEXT_PUBLIC_JANUA_LOGOUT_PATH || '/logout'

/**
 * Post-logout landing.
 *
 * janua's `validate_post_logout_redirect_uri` accepts either an exact
 * registered redirect URI or the ORIGIN ROOT (path `/`) of a registered
 * callback. This console only registers `${origin}/auth/callback`, so a bare
 * `${origin}/login` would be rejected with `400 invalid_request`. We therefore
 * point the redirect at the origin root, which janua accepts; middleware then
 * bounces the now-cookieless browser from `/` to `/login`.
 */
function postLogoutRedirectUri(): string {
  const origin = typeof window !== 'undefined' ? window.location.origin : ''
  return `${origin}/`
}

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

/**
 * OIDC `prompt` values this console uses.
 *
 * - `login` — «Sign in as someone else»: force Janua to re-authenticate even
 *   if an SSO session exists.
 * - `select_account` — «Switch account»: ask Janua to show its account chooser.
 *
 * Omitting `prompt` entirely is the default «Sign in with Janua SSO» behavior
 * (silent reuse of an existing session). See RFC 6749 / OIDC Core §3.1.2.1.
 */
export type LoginPrompt = 'login' | 'select_account'

interface LoginOptions {
  prompt?: LoginPrompt
}

interface AuthContextType {
  user: User | null
  isLoading: boolean
  isAuthenticated: boolean
  isAuthorized: boolean
  login: (options?: LoginOptions) => void
  /**
   * Two-tab focus: open a NEW browser tab and run Janua's account chooser in it,
   * so the operator can bring up a second, different estate account alongside
   * this one. The new tab runs `prompt=select_account`; picking an account there
   * pins that tab to it (via the `#janua_sid` fragment) without disturbing this
   * tab. See `lib/tab-session.ts`.
   */
  openAccountInNewTab: () => void
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
      // Two-tab focus: if we arrived carrying `#janua_sid=<sid>` (from Janua's
      // switch-session?return_sid), pin THIS tab to that estate account and strip
      // the fragment from the address bar. A no-op when no fragment is present.
      adoptSidFromLocation()

      const token = document.cookie.split('; ').find(r => r.startsWith('dispatch_auth='))?.split('=')[1]
      if (!token) {
        setIsLoading(false)
        return
      }

      // Verify token with Janua. `withTabSessionHeader` adds `X-Janua-Session`
      // only when this tab holds a per-tab sid; otherwise the headers are
      // unchanged. (The Bearer already names the user, so the header is
      // redundant here — it is sent for contract consistency with the authorize
      // path, and is harmless because Bearer outranks it in Janua's resolver.)
      const response = await fetch(`${JANUA_URL}/api/v1/auth/me`, {
        headers: withTabSessionHeader({
          Authorization: `Bearer ${token}`,
        }),
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

  const login = useCallback(async (options?: LoginOptions) => {
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

    // Optional OIDC `prompt`. Appended only when a value is supplied, so the
    // default sign-in keeps sending NO prompt (silent session reuse). Until
    // Janua honors these prompts, `select_account` degrades to today's silent
    // reuse — acceptable; it becomes a real chooser once the janua lane ships.
    if (options?.prompt) {
      params.set('prompt', options.prompt)
    }

    window.location.href = `${JANUA_URL}/api/v1/oauth/authorize?${params.toString()}`
  }, [])

  const openAccountInNewTab = useCallback(() => {
    if (typeof window === 'undefined') return
    // Open a NEW tab and let IT run the chooser. The authorize flow needs a PKCE
    // `code_verifier` in the tab's own (per-tab) sessionStorage, so the new tab
    // must start the flow itself — the opener cannot seed it. `/auth/new-account`
    // is a thin bootstrap page that calls `login({ prompt: 'select_account' })`
    // on mount. Opened with noopener so the new tab cannot script this one.
    window.open('/auth/new-account', '_blank', 'noopener,noreferrer')
  }, [])

  const logout = useCallback(async () => {
    // Clear the local `dispatch_*` cookies FIRST so the browser is already
    // signed out of this console before we hand off to Janua. If the top-level
    // navigation below never resolves to a working page (e.g. janua's logout
    // endpoint is not deployed yet), the local session is still gone.
    const hostname = typeof window !== 'undefined' ? window.location.hostname : ''
    const clearDomain = hostname.includes('.enclii.dev') ? '; domain=.enclii.dev' : ''
    document.cookie = `dispatch_auth=; Max-Age=0; path=/${clearDomain}`
    document.cookie = `dispatch_user_email=; Max-Age=0; path=/${clearDomain}`
    document.cookie = `dispatch_user_roles=; Max-Age=0; path=/${clearDomain}`
    setUser(null)

    // RP-initiated logout: a TOP-LEVEL browser navigation (not fetch) to Janua's
    // end_session endpoint. This is the only thing that actually deletes the
    // `janua_sso` cookie and revokes the session row — a same-origin cookie a
    // cross-origin fetch cannot touch. Janua then 302s back to the origin root,
    // and middleware sends the cookieless browser on to /login.
    //
    // Graceful degrade: until the janua lane deploys this GET endpoint, the URL
    // 404s. Because a cross-origin 404 cannot self-redirect us back, we cannot
    // guarantee the /login landing purely from the client in that window; the
    // local cookies are already cleared above, so re-opening the console lands
    // on /login regardless. To keep the common (janua-down) case from stranding
    // the operator on a broken page, fall back to a same-tab /login redirect if
    // the origin can't be determined.
    if (typeof window === 'undefined') {
      router.push('/login')
      return
    }

    const params = new URLSearchParams({
      client_id: OAUTH_CLIENT_ID,
      post_logout_redirect_uri: postLogoutRedirectUri(),
    })
    window.location.href = `${JANUA_URL}${JANUA_LOGOUT_PATH}?${params.toString()}`
  }, [router])

  return (
    <AuthContext.Provider
      value={{
        user,
        isLoading,
        isAuthenticated: !!user,
        isAuthorized,
        login,
        openAccountInNewTab,
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
