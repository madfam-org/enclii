/**
 * Tests for app/auth/callback/page.tsx — the PKCE callback exchanges the
 * authorization code only when the returned OAuth `state` matches the value
 * this tab stored when sign-in started.
 */

import React from 'react'
import { render, screen, waitFor } from '@testing-library/react'
import { OAUTH_STATE_KEY, storeOAuthState } from '@/lib/oauth-state'

let mockSearch = ''
const mockPush = jest.fn()
jest.mock('next/navigation', () => ({
  useSearchParams: () => new URLSearchParams(mockSearch),
  useRouter: () => ({ push: mockPush, replace: jest.fn(), prefetch: jest.fn(), back: jest.fn() }),
}))

import AuthCallbackPage from '@/app/auth/callback/page'

const STATE = 'stored-state-value'
const mockFetch = jest.fn()

let href = ''
let originalLocation: Location

function okJson(body: unknown) {
  return Promise.resolve({ ok: true, json: () => Promise.resolve(body) })
}

function tokenExchangeCalls() {
  return mockFetch.mock.calls.filter(([url]) => String(url).endsWith('/api/v1/oauth/token'))
}

beforeEach(() => {
  sessionStorage.clear()
  mockPush.mockReset()
  mockFetch.mockReset()
  global.fetch = mockFetch as unknown as typeof fetch
  mockFetch.mockImplementation((url: string) => {
    if (url.endsWith('/api/v1/oauth/token')) return okJson({ access_token: 'at' })
    if (url.endsWith('/api/v1/auth/me')) {
      return okJson({ id: 'u1', email: 'operator@example.org', roles: ['admin'] })
    }
    return okJson({})
  })

  sessionStorage.setItem('dispatch_code_verifier', 'verifier-value')
  storeOAuthState(STATE)

  href = ''
  originalLocation = window.location
  Object.defineProperty(window, 'location', {
    value: {
      ...originalLocation,
      origin: 'https://admin.example.test',
      hostname: 'admin.example.test',
      hash: '',
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
})

describe('OAuth callback — state verification', () => {
  it('exchanges the code when the returned state matches the stored one', async () => {
    mockSearch = `code=auth-code&state=${STATE}`
    render(<AuthCallbackPage />)

    await waitFor(() => expect(screen.getByText('Welcome, Operator')).toBeInTheDocument())
    const calls = tokenExchangeCalls()
    expect(calls).toHaveLength(1)
    const body = calls[0][1].body as URLSearchParams
    expect(body.get('code')).toBe('auth-code')
    expect(body.get('code_verifier')).toBe('verifier-value')
  })

  it('clears the stored state after use', async () => {
    mockSearch = `code=auth-code&state=${STATE}`
    render(<AuthCallbackPage />)

    await waitFor(() => expect(screen.getByText('Welcome, Operator')).toBeInTheDocument())
    expect(sessionStorage.getItem(OAUTH_STATE_KEY)).toBeNull()
    expect(sessionStorage.getItem('dispatch_code_verifier')).toBeNull()
  })

  it('does not exchange the code when the state is missing', async () => {
    mockSearch = 'code=auth-code'
    render(<AuthCallbackPage />)

    await waitFor(() => expect(screen.getByText('Authentication Failed')).toBeInTheDocument())
    expect(screen.getByText(/start sign-in again/)).toBeInTheDocument()
    expect(mockFetch).not.toHaveBeenCalled()
    expect(sessionStorage.getItem(OAUTH_STATE_KEY)).toBeNull()
    expect(sessionStorage.getItem('dispatch_code_verifier')).toBeNull()
  })

  it('does not exchange the code when the state does not match', async () => {
    mockSearch = 'code=auth-code&state=some-other-value'
    render(<AuthCallbackPage />)

    await waitFor(() => expect(screen.getByText('Authentication Failed')).toBeInTheDocument())
    expect(mockFetch).not.toHaveBeenCalled()
    expect(sessionStorage.getItem(OAUTH_STATE_KEY)).toBeNull()
    expect(sessionStorage.getItem('dispatch_code_verifier')).toBeNull()
  })

  it('does not exchange the code when this tab never started a sign-in', async () => {
    sessionStorage.removeItem(OAUTH_STATE_KEY)
    mockSearch = `code=auth-code&state=${STATE}`
    render(<AuthCallbackPage />)

    await waitFor(() => expect(screen.getByText('Authentication Failed')).toBeInTheDocument())
    expect(mockFetch).not.toHaveBeenCalled()
  })

  it('clears the pending sign-in when the provider returns an error', async () => {
    mockSearch = 'error=access_denied'
    render(<AuthCallbackPage />)

    await waitFor(() => expect(screen.getByText('Authentication Failed')).toBeInTheDocument())
    expect(mockFetch).not.toHaveBeenCalled()
    expect(sessionStorage.getItem(OAUTH_STATE_KEY)).toBeNull()
    expect(sessionStorage.getItem('dispatch_code_verifier')).toBeNull()
  })
})
