/**
 * Tests for app/auth/callback/page.tsx — the PKCE callback exchanges the
 * authorization code only when the returned OAuth `state` matches the value
 * this tab stored when sign-in started.
 */

import React from 'react'
import { render, screen, waitFor } from '@testing-library/react'
import { OAUTH_STATE_KEY, storeOAuthState } from '@/lib/oauth-state'

let mockSearch = ''
jest.mock('next/navigation', () => ({
  useSearchParams: () => new URLSearchParams(mockSearch),
}))
jest.mock('@/lib/analytics/posthog', () => ({ identifyUser: jest.fn() }))

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
  localStorage.clear()
  mockFetch.mockReset()
  global.fetch = mockFetch as unknown as typeof fetch
  mockFetch.mockImplementation((url: string) => {
    if (url.endsWith('/api/v1/oauth/token')) return okJson({ access_token: 'at', expires_in: 900 })
    if (url.endsWith('/api/v1/auth/me')) return okJson({ id: 'u1', email: 'user@example.org' })
    return okJson({})
  })

  sessionStorage.setItem('enclii_code_verifier', 'verifier-value')
  storeOAuthState(STATE)

  href = ''
  originalLocation = window.location
  Object.defineProperty(window, 'location', {
    value: {
      ...originalLocation,
      origin: 'https://app.example.test',
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
  jest.useRealTimers()
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

    await waitFor(() => expect(screen.getByText('Authentication successful!')).toBeInTheDocument())
    const calls = tokenExchangeCalls()
    expect(calls).toHaveLength(1)
    const body = calls[0][1].body as URLSearchParams
    expect(body.get('code')).toBe('auth-code')
    expect(body.get('code_verifier')).toBe('verifier-value')
  })

  it('clears the stored state after use', async () => {
    mockSearch = `code=auth-code&state=${STATE}`
    render(<AuthCallbackPage />)

    await waitFor(() => expect(screen.getByText('Authentication successful!')).toBeInTheDocument())
    expect(sessionStorage.getItem(OAUTH_STATE_KEY)).toBeNull()
    expect(sessionStorage.getItem('enclii_code_verifier')).toBeNull()
  })

  it('does not exchange the code when the state is missing', async () => {
    mockSearch = 'code=auth-code'
    render(<AuthCallbackPage />)

    await waitFor(() => expect(screen.getByText('Authentication failed')).toBeInTheDocument())
    expect(screen.getByText(/start sign-in again/)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Try again' })).toBeInTheDocument()
    expect(mockFetch).not.toHaveBeenCalled()
    expect(sessionStorage.getItem(OAUTH_STATE_KEY)).toBeNull()
    expect(sessionStorage.getItem('enclii_code_verifier')).toBeNull()
  })

  it('does not exchange the code when the state does not match', async () => {
    mockSearch = 'code=auth-code&state=some-other-value'
    render(<AuthCallbackPage />)

    await waitFor(() => expect(screen.getByText('Authentication failed')).toBeInTheDocument())
    expect(mockFetch).not.toHaveBeenCalled()
    expect(sessionStorage.getItem(OAUTH_STATE_KEY)).toBeNull()
    expect(sessionStorage.getItem('enclii_code_verifier')).toBeNull()
  })

  it('does not exchange the code when this tab never started a sign-in', async () => {
    sessionStorage.removeItem(OAUTH_STATE_KEY)
    mockSearch = `code=auth-code&state=${STATE}`
    render(<AuthCallbackPage />)

    await waitFor(() => expect(screen.getByText('Authentication failed')).toBeInTheDocument())
    expect(mockFetch).not.toHaveBeenCalled()
  })

  it('clears the pending sign-in when the provider returns an error', async () => {
    const consoleError = jest.spyOn(console, 'error').mockImplementation(() => {})
    mockSearch = 'error=access_denied'
    render(<AuthCallbackPage />)

    await waitFor(() => expect(screen.getByText('Authentication failed')).toBeInTheDocument())
    expect(mockFetch).not.toHaveBeenCalled()
    expect(sessionStorage.getItem(OAUTH_STATE_KEY)).toBeNull()
    expect(sessionStorage.getItem('enclii_code_verifier')).toBeNull()
    consoleError.mockRestore()
  })
})

describe('OAuth callback — return path', () => {
  async function completeSignInWithReturn(stored: string | null) {
    jest.useFakeTimers()
    if (stored !== null) localStorage.setItem('auth_return_url', stored)
    mockSearch = `code=auth-code&state=${STATE}`
    render(<AuthCallbackPage />)
    await waitFor(() => expect(screen.getByText('Authentication successful!')).toBeInTheDocument())
    jest.advanceTimersByTime(600)
    return href
  }

  it('returns to the stored same-origin path', async () => {
    expect(await completeSignInWithReturn('/projects/a?tab=logs')).toBe('/projects/a?tab=logs')
    expect(localStorage.getItem('auth_return_url')).toBeNull()
  })

  it('falls back to / for an off-origin return URL', async () => {
    expect(await completeSignInWithReturn('https://other.example.test/x')).toBe('/')
  })

  it('falls back to / when no return path was stored', async () => {
    expect(await completeSignInWithReturn(null)).toBe('/')
  })
})
