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
      <button data-testid="login-btn" onClick={() => login()}>Login</button>
      <button data-testid="login-default-btn" onClick={() => login()}>Login default</button>
      <button data-testid="login-select-btn" onClick={() => login({ prompt: 'select_account' })}>Switch account</button>
      <button data-testid="login-prompt-btn" onClick={() => login({ prompt: 'login' })}>Sign in as someone else</button>
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
  it('clears user state and redirects to /login', async () => {
    Object.defineProperty(document, 'cookie', {
      writable: true,
      value: 'dispatch_auth=valid-jwt-token',
      configurable: true,
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

    render(
      <AuthProvider>
        <TestConsumer />
      </AuthProvider>
    )

    await waitFor(() => {
      expect(screen.getByTestId('authenticated')).toHaveTextContent('true')
    })

    // Logout - mock the Janua logout call
    mockFetch.mockResolvedValueOnce({ ok: true })

    await act(async () => {
      screen.getByTestId('logout-btn').click()
    })

    await waitFor(() => {
      expect(screen.getByTestId('user')).toHaveTextContent('null')
    })
    expect(mockPush).toHaveBeenCalledWith('/login')
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

  // ---------------------------------------------------------------------------
  // Layer 2 — Switch account / Sign in as someone else (OIDC `prompt`)
  // ---------------------------------------------------------------------------

  describe('prompt (account switching)', () => {
    let originalCrypto: Crypto
    let originalLocation: Location
    let href: string

    beforeEach(() => {
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

      href = ''
      originalLocation = window.location
      Object.defineProperty(window, 'location', {
        value: {
          ...originalLocation,
          origin: 'https://admin.enclii.dev',
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
      expect(url.searchParams.get('prompt')).toBe('select_account')
      // core PKCE params are still present
      expect(url.searchParams.get('response_type')).toBe('code')
      expect(url.searchParams.get('code_challenge_method')).toBe('S256')
    })

    it('appends prompt=login for «Sign in as someone else»', async () => {
      const url = await clickAndReadAuthorizeUrl('login-prompt-btn')
      expect(url.searchParams.get('prompt')).toBe('login')
    })

    it('sends NO prompt for the default sign-in', async () => {
      const url = await clickAndReadAuthorizeUrl('login-default-btn')
      expect(url.searchParams.has('prompt')).toBe(false)
      expect(url.searchParams.get('response_type')).toBe('code')
    })
  })
})
