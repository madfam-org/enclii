/**
 * Tests for lib/tab-session.ts — per-tab estate session (two-tab focus).
 *
 * The invariant under test: the per-tab sid is a REFERENCE the tab remembers so
 * it can assert `X-Janua-Session`; it is read from a fragment (never a server
 * round trip) and stored per-tab. These pins keep the fragment parsing, the
 * per-tab storage, and the additive header behavior honest.
 */

import {
  TAB_SID_KEY,
  TAB_SESSION_HEADER,
  getTabSid,
  setTabSid,
  readSidFromFragment,
  adoptSidFromLocation,
  withTabSessionHeader,
} from '@/lib/tab-session'

beforeEach(() => {
  window.sessionStorage.clear()
})

describe('readSidFromFragment', () => {
  it('extracts the sid from a #janua_sid=<sid> fragment', () => {
    expect(readSidFromFragment('#janua_sid=abc-123')).toBe('abc-123')
  })

  it('accepts the fragment without a leading #', () => {
    expect(readSidFromFragment('janua_sid=abc-123')).toBe('abc-123')
  })

  it('finds the sid among other fragment params', () => {
    expect(readSidFromFragment('#foo=1&janua_sid=abc-123&bar=2')).toBe('abc-123')
  })

  it('returns null for an empty or unrelated fragment', () => {
    expect(readSidFromFragment('')).toBeNull()
    expect(readSidFromFragment('#')).toBeNull()
    expect(readSidFromFragment('#other=x')).toBeNull()
    expect(readSidFromFragment(null)).toBeNull()
    expect(readSidFromFragment(undefined)).toBeNull()
  })
})

describe('getTabSid / setTabSid', () => {
  it('round-trips a sid through per-tab sessionStorage', () => {
    expect(getTabSid()).toBeNull()
    setTabSid('sid-1')
    expect(getTabSid()).toBe('sid-1')
    expect(window.sessionStorage.getItem(TAB_SID_KEY)).toBe('sid-1')
  })

  it('clears the sid when set to null or empty', () => {
    setTabSid('sid-1')
    setTabSid(null)
    expect(getTabSid()).toBeNull()
    setTabSid('sid-2')
    setTabSid('   ')
    expect(getTabSid()).toBeNull()
  })

  it('trims stored sids', () => {
    setTabSid('  sid-3  ')
    expect(getTabSid()).toBe('sid-3')
  })
})

describe('adoptSidFromLocation', () => {
  const origHash = window.location.hash

  afterEach(() => {
    window.location.hash = origHash
    window.history.replaceState(null, '', '/')
  })

  it('pins the tab to the fragment sid and strips the fragment', () => {
    window.history.replaceState(null, '', '/auth/callback?code=x#janua_sid=sid-9')
    const adopted = adoptSidFromLocation()
    expect(adopted).toBe('sid-9')
    expect(getTabSid()).toBe('sid-9')
    // Fragment removed; path + query preserved.
    expect(window.location.hash).toBe('')
    expect(window.location.pathname).toBe('/auth/callback')
    expect(window.location.search).toBe('?code=x')
  })

  it('is a no-op when there is no janua_sid fragment', () => {
    window.history.replaceState(null, '', '/somewhere')
    expect(adoptSidFromLocation()).toBeNull()
    expect(getTabSid()).toBeNull()
  })
})

describe('withTabSessionHeader', () => {
  it('adds X-Janua-Session only when the tab holds a sid', () => {
    expect(withTabSessionHeader({ Authorization: 'Bearer t' })).toEqual({
      Authorization: 'Bearer t',
    })
    setTabSid('sid-7')
    expect(withTabSessionHeader({ Authorization: 'Bearer t' })).toEqual({
      Authorization: 'Bearer t',
      [TAB_SESSION_HEADER]: 'sid-7',
    })
  })

  it('does not mutate the caller headers', () => {
    setTabSid('sid-7')
    const base = { Authorization: 'Bearer t' }
    withTabSessionHeader(base)
    expect(base).toEqual({ Authorization: 'Bearer t' })
  })

  it('defaults to an empty header set', () => {
    expect(withTabSessionHeader()).toEqual({})
    setTabSid('sid-7')
    expect(withTabSessionHeader()).toEqual({ [TAB_SESSION_HEADER]: 'sid-7' })
  })
})
