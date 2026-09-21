/**
 * Tests for contexts/AuthContext.tsx
 *
 * Tests the AuthProvider and useAuth hook, including:
 * - Provider rendering
 * - Authentication state management
 * - Domain and role authorization validation
 * - Login PKCE flow initiation
 * - Logout and cookie clearing
 * - useAuth outside provider error
 */

import React from 'react'
import { render, screen, waitFor, act } from '@testing-library/react'

// Mock next/navigation
const mockPush = jest.fn()
jest.mock('next/navigation', () => ({
  useRouter: () => ({
    push: mockPush,
    replace: jest.fn(),
    prefetch: jest.fn(),
    back: jest.fn(),
  }),
}))

import { AuthProvider, useAuth } from '@/contexts/AuthContext'

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

const mockFetch = jest.fn()
global.fetch = mockFetch

// A test component that consumes useAuth
function TestConsumer() {
  const { user, isLoading, isAuthenticated, isAuthorized, error, login, logout } = useAuth()

  return (
    <div>
      <div data-testid="loading">{String(isLoading)}</div>
      <div data-testid="authenticated">{String(isAuthenticated)}</div>
      <div data-testid="authorized">{String(isAuthorized)}</div>
      <div data-testid="user">{user ? JSON.stringify(user) : 'null'}</div>
      <div data-testid="error">{error || 'none'}</div>
      <button data-testid="login-btn" onClick={login}>Login</button>
      <button data-testid="logout-btn" onClick={logout}>Logout</button>
    </div>
  )
}

beforeEach(() => {
  mockFetch.mockReset()
  mockPush.mockReset()
  // Reset document.cookie to empty
  Object.defineProperty(document, 'cookie', {
    writable: true,
    value: '',
    configurable: true,
  })
})

// =============================================================================
// Provider rendering
// =============================================================================

describe('AuthProvider', () => {
  it('renders children', async () => {
    render(
      <AuthProvider>
        <div data-testid="child">Hello</div>
      </AuthProvider>
    )

    expect(screen.getByTestId('child')).toHaveTextContent('Hello')
  })

  it('shows not authenticated when no cookie is present', async () => {
    render(
      <AuthProvider>
        <TestConsumer />
      </AuthProvider>
    )

    await waitFor(() => {
      expect(screen.getByTestId('loading')).toHaveTextContent('false')
    })
    expect(screen.getByTestId('authenticated')).toHaveTextContent('false')
    expect(screen.getByTestId('user')).toHaveTextContent('null')
  })

  it('sets user after successful auth check with valid domain and role', async () => {
    Object.defineProperty(document, 'cookie', {
      writable: true,
      value: 'dispatch_auth=valid-jwt-token',
      configurable: true,
    })

    mockFetch.mockResolvedValueOnce({
      ok: true,
      json: () => Promise.resolve({
        id: 'user-1',
        email: 'admin@example.org',
        name: 'Admin User',
        is_admin: false,
        roles: ['admin'],
      }),
    })

    render(
      <AuthProvider>
        <TestConsumer />
      </AuthProvider>
    )

    await waitFor(() => {
      expect(screen.getByTestId('loading')).toHaveTextContent('false')
    })

    expect(screen.getByTestId('authenticated')).toHaveTextContent('true')
    const userData = JSON.parse(screen.getByTestId('user').textContent || '{}')
    expect(userData.email).toBe('admin@example.org')
    expect(userData.roles).toContain('admin')
  })

  it('adds admin role when is_admin flag is true but admin not in roles', async () => {
    Object.defineProperty(document, 'cookie', {
      writable: true,
      value: 'dispatch_auth=valid-jwt-token',
      configurable: true,
    })

    mockFetch.mockResolvedValueOnce({
      ok: true,
      json: () => Promise.resolve({
        id: 'user-2',
        email: 'admin@example.org',
        name: 'Admin User',
        is_admin: true,
        roles: ['operator'],
      }),
    })

    render(
      <AuthProvider>
        <TestConsumer />
      </AuthProvider>
    )

    await waitFor(() => {
      expect(screen.getByTestId('loading')).toHaveTextContent('false')
    })

    const userData = JSON.parse(screen.getByTestId('user').textContent || '{}')
    expect(userData.roles).toContain('admin')
    expect(userData.roles).toContain('operator')
  })

  it('sets error for unauthorized domain', async () => {
    Object.defineProperty(document, 'cookie', {
      writable: true,
      value: 'dispatch_auth=valid-jwt-token',
      configurable: true,
    })

    mockFetch.mockResolvedValueOnce({
      ok: true,
      json: () => Promise.resolve({
        id: 'user-3',
        email: 'hacker@evil.com',
        name: 'Bad Actor',
        is_admin: false,
        roles: ['admin'],
      }),
    })

    render(
      <AuthProvider>
        <TestConsumer />
      </AuthProvider>
    )

    await waitFor(() => {
      expect(screen.getByTestId('loading')).toHaveTextContent('false')
    })

    expect(screen.getByTestId('user')).toHaveTextContent('null')
    expect(screen.getByTestId('error').textContent).toContain('email domain is not authorized')
  })

  it('sets error for unauthorized role', async () => {
    Object.defineProperty(document, 'cookie', {
      writable: true,
      value: 'dispatch_auth=valid-jwt-token',
      configurable: true,
    })

    mockFetch.mockResolvedValueOnce({
      ok: true,
      json: () => Promise.resolve({
        id: 'user-4',
        email: 'viewer@example.org',
        name: 'Viewer',
        is_admin: false,
        roles: ['viewer'],
      }),
    })

    render(
      <AuthProvider>
        <TestConsumer />
      </AuthProvider>
    )

    await waitFor(() => {
      expect(screen.getByTestId('loading')).toHaveTextContent('false')
    })

    expect(screen.getByTestId('user')).toHaveTextContent('null')
    expect(screen.getByTestId('error').textContent).toContain('required role')
  })

  it('clears user on failed auth response (non-ok)', async () => {
    Object.defineProperty(document, 'cookie', {
      writable: true,
      value: 'dispatch_auth=expired-jwt',
      configurable: true,
    })

    mockFetch.mockResolvedValueOnce({
      ok: false,
      status: 401,
    })

    render(
      <AuthProvider>
        <TestConsumer />
      </AuthProvider>
    )

    await waitFor(() => {
      expect(screen.getByTestId('loading')).toHaveTextContent('false')
    })

    expect(screen.getByTestId('authenticated')).toHaveTextContent('false')
    expect(screen.getByTestId('user')).toHaveTextContent('null')
  })

  it('sets error on auth check network failure', async () => {
    Object.defineProperty(document, 'cookie', {
      writable: true,
      value: 'dispatch_auth=valid-jwt',
      configurable: true,
    })

    // Suppress the expected console.error from AuthContext
    const spy = jest.spyOn(console, 'error').mockImplementation(() => {})

    mockFetch.mockRejectedValueOnce(new Error('Network error'))

    render(
      <AuthProvider>
        <TestConsumer />
      </AuthProvider>
    )

    await waitFor(() => {
      expect(screen.getByTestId('loading')).toHaveTextContent('false')
    })

    expect(screen.getByTestId('error')).toHaveTextContent('Authentication failed')

    spy.mockRestore()
  })
})

// =============================================================================
// logout
// =============================================================================

describe('logout', () => {
  const JANUA_URL = 'https://auth.madfam.io'

  function mockLocation(origin: string) {
    const originalLocation = window.location
    let href = ''
    Object.defineProperty(window, 'location', {
      value: {
        ...originalLocation,
        origin,
        hostname: new URL(origin).hostname,
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
    return {
      getHref: () => href,
      restore: () =>
        Object.defineProperty(window, 'location', {
          value: originalLocation,
          writable: true,
          configurable: true,
        }),
    }
  }

  it('performs a top-level navigation to the Janua RP-initiated logout URL with the encoded return, after clearing local cookies', async () => {
    // The naive string `document.cookie` mock used elsewhere in this file
    // OVERWRITES on every assignment, so it cannot show three cleared cookies
    // at once. Track every WRITE instead — that is what lets us assert both the
    // clearing AND the ordering (all cookies cleared before the redirect).
    const cookieWrites: string[] = []
    let cookieValue = 'dispatch_auth=valid-jwt-token'
    Object.defineProperty(document, 'cookie', {
      configurable: true,
      get() {
        return cookieValue
      },
      set(v: string) {
        cookieWrites.push(v)
        cookieValue = v
      },
    })

    // Auth check succeeds
    mockFetch.mockResolvedValueOnce({
      ok: true,
      json: () => Promise.resolve({
        id: 'user-1',
        email: 'admin@example.org',
        roles: ['admin'],
      }),
    })

    const loc = mockLocation('https://admin.enclii.dev')

    render(
      <AuthProvider>
        <TestConsumer />
      </AuthProvider>
    )

    await waitFor(() => {
      expect(screen.getByTestId('authenticated')).toHaveTextContent('true')
    })

    // No fetch is expected during logout anymore — reset the mock so a stray
    // call would be visible.
    mockFetch.mockReset()
    cookieWrites.length = 0

    await act(async () => {
      screen.getByTestId('logout-btn').click()
    })

    await waitFor(() => {
      expect(screen.getByTestId('user')).toHaveTextContent('null')
    })

    // All three local cookies were cleared with Max-Age=0 before handing off.
    const cleared = cookieWrites.filter((w) => w.includes('Max-Age=0'))
    expect(cleared.some((w) => w.startsWith('dispatch_auth='))).toBe(true)
    expect(cleared.some((w) => w.startsWith('dispatch_user_email='))).toBe(true)
    expect(cleared.some((w) => w.startsWith('dispatch_user_roles='))).toBe(true)

    // Top-level navigation to Janua end_session with the encoded origin-root return.
    const href = loc.getHref()
    expect(href.startsWith(`${JANUA_URL}/logout?`)).toBe(true)
    const url = new URL(href)
    expect(url.searchParams.get('client_id')).toBeTruthy()
    expect(url.searchParams.get('post_logout_redirect_uri')).toBe('https://admin.enclii.dev/')
    // Encoded in the raw query string (not a bare unescaped URI).
    expect(href).toContain(
      `post_logout_redirect_uri=${encodeURIComponent('https://admin.enclii.dev/')}`
    )

    // logout() must NOT use fetch to end the Janua session (fetch cannot delete
    // the same-origin janua_sso cookie).
    expect(mockFetch).not.toHaveBeenCalled()

    loc.restore()
  })
})

// =============================================================================
// useAuth outside provider
// =============================================================================

describe('useAuth', () => {
  it('throws when used outside AuthProvider', () => {
    // Suppress console.error for the expected React error boundary output
    const spy = jest.spyOn(console, 'error').mockImplementation(() => {})

    function BadComponent() {
      useAuth()
      return null
    }

    expect(() => render(<BadComponent />)).toThrow(
      'useAuth must be used within an AuthProvider'
    )

    spy.mockRestore()
  })
})

// =============================================================================
// login
// =============================================================================

describe('login', () => {
  it('stores code verifier in sessionStorage when login is called', async () => {
    // Mock crypto APIs for PKCE
    const mockGetRandomValues = jest.fn((arr: Uint8Array) => {
      for (let i = 0; i < arr.length; i++) arr[i] = i % 256
      return arr
    })
    const originalCrypto = global.crypto
    Object.defineProperty(global, 'crypto', {
      value: {
        getRandomValues: mockGetRandomValues,
        subtle: {
          digest: jest.fn().mockResolvedValue(new ArrayBuffer(32)),
        },
      },
      writable: true,
      configurable: true,
    })

    const mockSetItem = jest.spyOn(Storage.prototype, 'setItem')

    // Mock window.location. `window.location` is declared with a
    // string-accepting setter (`set location(href: string)`), so a plain
    // assignment of a Location object does not type-check; define the
    // property instead, as done for `crypto` above.
    const originalLocation = window.location
    Object.defineProperty(window, 'location', {
      value: {
        ...originalLocation,
        origin: 'https://admin.enclii.dev',
        href: '',
      } as Location,
      writable: true,
      configurable: true,
    })

    render(
      <AuthProvider>
        <TestConsumer />
      </AuthProvider>
    )

    await waitFor(() => {
      expect(screen.getByTestId('loading')).toHaveTextContent('false')
    })

    await act(async () => {
      screen.getByTestId('login-btn').click()
    })

    expect(mockSetItem).toHaveBeenCalledWith('dispatch_code_verifier', expect.any(String))

    mockSetItem.mockRestore()
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
})
