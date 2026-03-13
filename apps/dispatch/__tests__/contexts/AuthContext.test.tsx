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

    // Mock window.location
    const originalLocation = window.location
    // @ts-expect-error - overriding location for test
    delete window.location
    window.location = {
      ...originalLocation,
      origin: 'https://admin.enclii.dev',
      href: '',
    } as Location

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
    window.location = originalLocation
    Object.defineProperty(global, 'crypto', {
      value: originalCrypto,
      writable: true,
      configurable: true,
    })
  })
})
