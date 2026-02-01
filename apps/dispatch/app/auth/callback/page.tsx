'use client'

import { useEffect, useState, Suspense } from 'react'
import { useRouter, useSearchParams } from 'next/navigation'
import { Radio, CheckCircle2, XCircle, Loader2 } from 'lucide-react'

/**
 * OAuth Callback Page
 *
 * Handles the redirect from Janua SSO after authentication.
 * Validates the user (domain + role) and stores the token.
 */

const JANUA_URL = process.env.NEXT_PUBLIC_JANUA_URL || 'https://auth.madfam.io'
const OAUTH_CLIENT_ID = process.env.NEXT_PUBLIC_OAUTH_CLIENT_ID || 'jnc_lofqyf9LQXG_OwENAIw89p_XvngkWMi-'

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

function isAllowedDomain(email: string): boolean {
  return ALLOWED_DOMAINS.some((domain) => email.toLowerCase().endsWith(domain.toLowerCase()))
}

function hasAllowedRole(roles: string[] | undefined): boolean {
  if (!roles || roles.length === 0) return false
  return roles.some((role) => ALLOWED_ROLES.includes(role))
}

type CallbackStatus = 'processing' | 'success' | 'error'

function AuthCallbackContent() {
  const [status, setStatus] = useState<CallbackStatus>('processing')
  const [error, setError] = useState<string | null>(null)
  const router = useRouter()
  const searchParams = useSearchParams()

  useEffect(() => {
    handleCallback()
  }, [])

  const handleCallback = async () => {
    try {
      const code = searchParams.get('code')
      const errorParam = searchParams.get('error')

      if (errorParam) {
        throw new Error(searchParams.get('error_description') || 'Authentication failed')
      }

      if (!code) {
        throw new Error('No authorization code received')
      }

      // Retrieve PKCE code_verifier from session storage
      const codeVerifier = sessionStorage.getItem('dispatch_code_verifier')
      if (!codeVerifier) {
        throw new Error('PKCE verification failed - no code verifier found')
      }
      // Clear the code verifier (single use)
      sessionStorage.removeItem('dispatch_code_verifier')

      // Exchange code for token via OAuth token endpoint (with PKCE)
      const response = await fetch(`${JANUA_URL}/api/v1/oauth/token`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
        body: new URLSearchParams({
          grant_type: 'authorization_code',
          code,
          redirect_uri: `${window.location.origin}/auth/callback`,
          client_id: OAUTH_CLIENT_ID,
          code_verifier: codeVerifier,
        }),
      })

      if (!response.ok) {
        const errorData = await response.json().catch(() => ({}))
        throw new Error(errorData.detail || 'Failed to exchange authorization code')
      }

      const { access_token } = await response.json()

      // Verify user identity
      const meResponse = await fetch(`${JANUA_URL}/api/v1/auth/me`, {
        headers: { Authorization: `Bearer ${access_token}` },
      })

      if (!meResponse.ok) {
        throw new Error('Failed to verify user identity')
      }

      const user = await meResponse.json()

      // Extract roles from user data
      const userRoles: string[] = user.roles || []
      if (user.is_admin && !userRoles.includes('admin')) {
        userRoles.push('admin')
      }

      // SECURITY: Validate domain AND role
      const domainOk = isAllowedDomain(user.email)
      const roleOk = hasAllowedRole(userRoles)

      if (!domainOk || !roleOk) {
        // Clear any stored data with proper domain
        const clearHostname = window.location.hostname
        const clearDomain = clearHostname.includes('.enclii.dev') ? '; domain=.enclii.dev' : ''
        document.cookie = `dispatch_auth=; Max-Age=0; path=/${clearDomain}`
        document.cookie = `dispatch_user_email=; Max-Age=0; path=/${clearDomain}`
        document.cookie = `dispatch_user_roles=; Max-Age=0; path=/${clearDomain}`

        const reason = !domainOk
          ? 'Your email domain is not authorized.'
          : 'You do not have the required operator role.'
        setError(`Access denied. ${reason}`)
        setStatus('error')

        // Redirect to access denied after delay
        setTimeout(() => {
          router.push('/access-denied')
        }, 2000)
        return
      }

      // Set cookies via server-side API route to ensure proper Set-Cookie headers
      // This avoids race conditions with client-side document.cookie
      const sessionResponse = await fetch('/api/auth/session', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          token: access_token,
          email: user.email,
          roles: userRoles.join(','),
        }),
      })

      if (!sessionResponse.ok) {
        throw new Error('Failed to establish session')
      }

      setStatus('success')

      // Full navigation ensures browser processes Set-Cookie headers before next page load
      setTimeout(() => {
        window.location.href = '/'
      }, 500)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Authentication failed')
      setStatus('error')

      // Redirect to login after delay
      setTimeout(() => {
        router.push('/login')
      }, 3000)
    }
  }

  return (
    <div className="min-h-screen flex items-center justify-center p-4">
      <div className="max-w-md w-full text-center space-y-6">
        {/* Logo */}
        <div className="inline-flex p-3 rounded-xl bg-primary/10 border border-primary/20">
          <Radio className="size-8 text-primary glow-effect" />
        </div>

        {/* Status */}
        <div className="space-y-4">
          {status === 'processing' && (
            <>
              <Loader2 className="size-8 mx-auto text-primary animate-spin" />
              <div>
                <h2 className="text-lg font-medium text-foreground">Authenticating</h2>
                <p className="text-sm text-muted-foreground">
                  Verifying your credentials<span className="terminal-cursor" />
                </p>
              </div>
            </>
          )}

          {status === 'success' && (
            <>
              <CheckCircle2 className="size-8 mx-auto text-status-success" />
              <div>
                <h2 className="text-lg font-medium text-foreground">Welcome, Operator</h2>
                <p className="text-sm text-muted-foreground">
                  Redirecting to Control Tower...
                </p>
              </div>
            </>
          )}

          {status === 'error' && (
            <>
              <XCircle className="size-8 mx-auto text-destructive" />
              <div>
                <h2 className="text-lg font-medium text-foreground">Authentication Failed</h2>
                <p className="text-sm text-destructive">{error}</p>
                <p className="text-xs text-muted-foreground mt-2">
                  Redirecting...
                </p>
              </div>
            </>
          )}
        </div>
      </div>
    </div>
  )
}

function AuthCallbackFallback() {
  return (
    <div className="min-h-screen flex items-center justify-center p-4">
      <div className="max-w-md w-full text-center space-y-6">
        <div className="inline-flex p-3 rounded-xl bg-primary/10 border border-primary/20">
          <Radio className="size-8 text-primary glow-effect" />
        </div>
        <div className="space-y-4">
          <Loader2 className="size-8 mx-auto text-primary animate-spin" />
          <div>
            <h2 className="text-lg font-medium text-foreground">Loading</h2>
            <p className="text-sm text-muted-foreground">
              Preparing authentication<span className="terminal-cursor" />
            </p>
          </div>
        </div>
      </div>
    </div>
  )
}

export default function AuthCallbackPage() {
  return (
    <Suspense fallback={<AuthCallbackFallback />}>
      <AuthCallbackContent />
    </Suspense>
  )
}
