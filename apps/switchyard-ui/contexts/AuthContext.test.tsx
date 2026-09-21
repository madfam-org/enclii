/**
 * Tests for contexts/AuthContext.tsx — OIDC account switching.
 *
 * Focused coverage for the account-switching model ported from the enclii
 * admin-console (DISPATCH) in #590: `loginWithOIDC()` appends the OIDC `prompt`
 * parameter to the Janua authorize URL only when supplied, so the default
 * sign-in keeps sending NO prompt (silent SSO-session reuse), «Switch account»
 * sends `prompt=select_account`, and «Sign in as someone else» sends
 * `prompt=login`.
 *
 * Mirrors apps/admin-console/__tests__/contexts/AuthContext.test.tsx →
 * `describe('prompt (account switching)')`. This file drives the OIDC provider;
 * the provider selects OIDC vs local from NEXT_PUBLIC_AUTH_MODE at module-load
 * time, so the module is (re)loaded inside jest.isolateModules AFTER the env
 * var is set (ES `import` is hoisted, so it cannot set env early enough).
 */

// The provider selects OIDC vs local from NEXT_PUBLIC_AUTH_MODE, read once at
// AuthContext module-load time. Set it BEFORE that module is first required.
// (A jest.mock factory is the only construct babel hoists above imports, so we
// use one to run the env assignment before AuthContext is resolved — React
// itself stays a single normal import so provider and renderer share one copy.)
jest.mock('@/contexts/AuthContext', () => {
  process.env.NEXT_PUBLIC_AUTH_MODE = 'oidc'
  return jest.requireActual('@/contexts/AuthContext')
})

import React from 'react'
import { render, screen, waitFor, act } from '@testing-library/react'
import { AuthProvider, useAuth } from '@/contexts/AuthContext'

const JANUA_BASE_URL = 'https://auth.madfam.io'

// A consumer exposing the three sign-in entry points the login page wires up.
function TestConsumer() {
  const { loginWithOIDC, isLoading } = useAuth()
  return (
    <div>
      <div data-testid="loading">{String(isLoading)}</div>
      <button data-testid="login-default-btn" onClick={() => loginWithOIDC()}>
        Sign in with Janua SSO
      </button>
      <button
        data-testid="login-select-btn"
        onClick={() => loginWithOIDC({ prompt: 'select_account' })}
      >
        Switch account
      </button>
      <button
        data-testid="login-prompt-btn"
        onClick={() => loginWithOIDC({ prompt: 'login' })}
      >
        Sign in as someone else
      </button>
    </div>
  )
}

describe('AuthContext OIDC login — prompt (account switching)', () => {
  let originalCrypto: Crypto
  let originalLocation: Location
  let href: string

  beforeEach(() => {
    // No cookie → checkAuth() finishes with isLoading=false and no /auth/me call.
    Object.defineProperty(document, 'cookie', {
      writable: true,
      value: '',
      configurable: true,
    })

    // Deterministic PKCE crypto.
    const mockGetRandomValues = jest.fn((arr: Uint8Array) => {
      for (let i = 0; i < arr.length; i++) arr[i] = i % 256
      return arr
    })
    originalCrypto = global.crypto
    Object.defineProperty(global, 'crypto', {
      value: {
        getRandomValues: mockGetRandomValues,
        subtle: { digest: jest.fn().mockResolvedValue(new ArrayBuffer(32)) },
      },
      writable: true,
      configurable: true,
    })

    // Capture the top-level navigation the provider performs on login.
    href = ''
    originalLocation = window.location
    Object.defineProperty(window, 'location', {
      value: {
        ...originalLocation,
        origin: 'https://app.enclii.dev',
        pathname: '/dashboard',
        get href() {
          return href
        },
        set href(value: string) {
          href = value
        },
      } as unknown as Location,
      writable: true,
      configurable: true,
    })
  })

  afterEach(() => {
    Object.defineProperty(window, 'location', {
      value: originalLocation,
      writable: true,
      configurable: true,
    })
    Object.defineProperty(global, 'crypto', {
      value: originalCrypto,
      writable: true,
      configurable: true,
    })
  })

  async function clickAndReadAuthorizeUrl(testId: string): Promise<URL> {
    render(
      <AuthProvider>
        <TestConsumer />
      </AuthProvider>
    )
    await waitFor(() => {
      expect(screen.getByTestId('loading')).toHaveTextContent('false')
    })
    await act(async () => {
      screen.getByTestId(testId).click()
    })
    expect(href).toContain('/api/v1/oauth/authorize?')
    return new URL(href)
  }

  it('appends prompt=select_account for «Switch account»', async () => {
    const url = await clickAndReadAuthorizeUrl('login-select-btn')
    expect(url.origin + url.pathname).toBe(`${JANUA_BASE_URL}/api/v1/oauth/authorize`)
    expect(url.searchParams.get('prompt')).toBe('select_account')
    // Core PKCE params are still present.
    expect(url.searchParams.get('response_type')).toBe('code')
    expect(url.searchParams.get('code_challenge_method')).toBe('S256')
    expect(url.searchParams.get('code_challenge')).toBeTruthy()
  })

  it('appends prompt=login for «Sign in as someone else»', async () => {
    const url = await clickAndReadAuthorizeUrl('login-prompt-btn')
    expect(url.searchParams.get('prompt')).toBe('login')
    expect(url.searchParams.get('response_type')).toBe('code')
  })

  it('sends NO prompt for the default sign-in (silent SSO reuse)', async () => {
    const url = await clickAndReadAuthorizeUrl('login-default-btn')
    expect(url.searchParams.has('prompt')).toBe(false)
    expect(url.searchParams.get('response_type')).toBe('code')
    expect(url.searchParams.get('code_challenge_method')).toBe('S256')
  })
})
