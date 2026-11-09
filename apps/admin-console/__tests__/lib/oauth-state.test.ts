/**
 * Tests for lib/oauth-state.ts — binding the OAuth `state` to the browser tab
 * and verifying it on the callback.
 */

import {
  OAUTH_STATE_KEY,
  OAUTH_STATE_TTL_MS,
  clearOAuthState,
  consumeOAuthState,
  generateOAuthState,
  safeReturnPath,
  storeOAuthState,
  timingSafeEqual,
} from '@/lib/oauth-state'

const ORIGIN = 'https://app.example.test'

beforeEach(() => {
  sessionStorage.clear()
})

describe('generateOAuthState', () => {
  it('returns 32 random bytes as URL-safe base64 (256 bits)', () => {
    const spy = jest.spyOn(crypto, 'getRandomValues')
    const state = generateOAuthState()
    expect(spy).toHaveBeenCalledTimes(1)
    expect((spy.mock.calls[0][0] as Uint8Array).length).toBe(32)
    expect(state).toMatch(/^[A-Za-z0-9_-]{43}$/)
    spy.mockRestore()
  })

  it('returns a different value on every call', () => {
    const values = new Set(Array.from({ length: 20 }, () => generateOAuthState()))
    expect(values.size).toBe(20)
  })
})

describe('consumeOAuthState', () => {
  it('accepts the stored value once and clears it', () => {
    storeOAuthState('abc123', 1_000)
    expect(consumeOAuthState('abc123', 2_000)).toBe(true)
    expect(sessionStorage.getItem(OAUTH_STATE_KEY)).toBeNull()
    expect(consumeOAuthState('abc123', 2_000)).toBe(false)
  })

  it('rejects a missing returned state and clears the stored value', () => {
    storeOAuthState('abc123', 1_000)
    expect(consumeOAuthState(null, 2_000)).toBe(false)
    expect(sessionStorage.getItem(OAUTH_STATE_KEY)).toBeNull()
  })

  it('rejects an empty returned state', () => {
    storeOAuthState('abc123', 1_000)
    expect(consumeOAuthState('', 2_000)).toBe(false)
  })

  it('rejects when no state was stored in this tab', () => {
    expect(consumeOAuthState('abc123')).toBe(false)
  })

  it('rejects a mismatched state and clears the stored value', () => {
    storeOAuthState('abc123', 1_000)
    expect(consumeOAuthState('abc124', 2_000)).toBe(false)
    expect(sessionStorage.getItem(OAUTH_STATE_KEY)).toBeNull()
  })

  it('rejects a stored state older than the TTL', () => {
    storeOAuthState('abc123', 1_000)
    expect(consumeOAuthState('abc123', 1_000 + OAUTH_STATE_TTL_MS + 1)).toBe(false)
  })

  it('rejects a malformed stored record', () => {
    sessionStorage.setItem(OAUTH_STATE_KEY, 'not-json')
    expect(consumeOAuthState('not-json')).toBe(false)
    sessionStorage.setItem(OAUTH_STATE_KEY, JSON.stringify({ v: '', t: Date.now() }))
    expect(consumeOAuthState('')).toBe(false)
  })

  it('clearOAuthState drops the pending value', () => {
    storeOAuthState('abc123')
    clearOAuthState()
    expect(sessionStorage.getItem(OAUTH_STATE_KEY)).toBeNull()
  })
})

describe('timingSafeEqual', () => {
  it('compares by value', () => {
    expect(timingSafeEqual('abc', 'abc')).toBe(true)
    expect(timingSafeEqual('abc', 'abd')).toBe(false)
    expect(timingSafeEqual('abc', 'abcd')).toBe(false)
    expect(timingSafeEqual('', '')).toBe(true)
  })
})

describe('safeReturnPath', () => {
  it.each([
    ['/projects', '/projects'],
    ['/projects/a?tab=logs#top', '/projects/a?tab=logs#top'],
    ['/', '/'],
  ])('keeps the same-origin relative path %s', (raw, expected) => {
    expect(safeReturnPath(raw, ORIGIN)).toBe(expected)
  })

  it.each([
    [null],
    [''],
    ['https://other.example.test/x'],
    ['//other.example.test/x'],
    ['/\\other.example.test/x'],
    ['\\\\other.example.test'],
    ['javascript:alert(1)'],
    ['projects'],
    ['/a\nb'],
    ['/auth/callback?code=x'],
  ])('falls back to / for %p', (raw) => {
    expect(safeReturnPath(raw, ORIGIN)).toBe('/')
  })
})
