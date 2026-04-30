/**
 * Tests for lib/analytics/posthog.ts
 *
 * Verifies PostHog initialization, user identification, reset, and event
 * tracking. All functions gracefully no-op when PostHog key is missing or
 * when running server-side.
 */

// Keep a stable reference to the mock functions across resetModules()
const mockInit = jest.fn()
const mockIdentify = jest.fn()
const mockReset = jest.fn()
const mockCapture = jest.fn()

jest.mock('posthog-js', () => ({
  __esModule: true,
  default: {
    init: mockInit,
    identify: mockIdentify,
    reset: mockReset,
    capture: mockCapture,
  },
}))

function getModule() {
  jest.resetModules()
  // Re-register the mock so the new module gets the same mock functions
  jest.doMock('posthog-js', () => ({
    __esModule: true,
    default: {
      init: mockInit,
      identify: mockIdentify,
      reset: mockReset,
      capture: mockCapture,
    },
  }))
  return require('@/lib/analytics/posthog')
}

beforeEach(() => {
  mockInit.mockClear()
  mockIdentify.mockClear()
  mockReset.mockClear()
  mockCapture.mockClear()
  delete (process.env as any).NEXT_PUBLIC_POSTHOG_KEY
  delete (process.env as any).NEXT_PUBLIC_POSTHOG_HOST
  Object.defineProperty(navigator, 'doNotTrack', { value: null, writable: true, configurable: true })
})

describe('initPostHog', () => {
  it('does not initialize without POSTHOG_KEY', () => {
    const { initPostHog } = getModule()
    initPostHog()
    expect(mockInit).not.toHaveBeenCalled()
  })

  it('initializes with correct config when key is set', () => {
    process.env.NEXT_PUBLIC_POSTHOG_KEY = 'phc_test_key'
    const { initPostHog } = getModule()
    initPostHog()
    expect(mockInit).toHaveBeenCalledWith('phc_test_key', {
      api_host: 'https://analytics.madfam.io',
      capture_pageview: false,
      autocapture: false,
      respect_dnt: true,
      persistence: 'localStorage+cookie',
      secure_cookie: true,
      disable_session_recording: true,
    })
  })

  it('uses custom host when POSTHOG_HOST is set', () => {
    process.env.NEXT_PUBLIC_POSTHOG_KEY = 'phc_test_key'
    process.env.NEXT_PUBLIC_POSTHOG_HOST = 'https://custom.host'
    const { initPostHog } = getModule()
    initPostHog()
    expect(mockInit).toHaveBeenCalledWith(
      'phc_test_key',
      expect.objectContaining({ api_host: 'https://custom.host' })
    )
  })

  it('respects Do Not Track', () => {
    process.env.NEXT_PUBLIC_POSTHOG_KEY = 'phc_test_key'
    Object.defineProperty(navigator, 'doNotTrack', { value: '1', writable: true, configurable: true })
    const { initPostHog } = getModule()
    initPostHog()
    expect(mockInit).not.toHaveBeenCalled()
  })

  it('only initializes once', () => {
    process.env.NEXT_PUBLIC_POSTHOG_KEY = 'phc_test_key'
    const { initPostHog } = getModule()
    initPostHog()
    initPostHog()
    expect(mockInit).toHaveBeenCalledTimes(1)
  })
})

describe('identifyUser', () => {
  it('does not call posthog.identify when not initialized', () => {
    const { identifyUser } = getModule()
    identifyUser('user-123', { role: 'admin' })
    expect(mockIdentify).not.toHaveBeenCalled()
  })

  it('calls posthog.identify after initialization', () => {
    process.env.NEXT_PUBLIC_POSTHOG_KEY = 'phc_test_key'
    const { initPostHog, identifyUser } = getModule()
    initPostHog()
    identifyUser('user-123', { role: 'admin' })
    expect(mockIdentify).toHaveBeenCalledWith('user-123', { role: 'admin' })
  })
})

describe('resetUser', () => {
  it('does not call posthog.reset when not initialized', () => {
    const { resetUser } = getModule()
    resetUser()
    expect(mockReset).not.toHaveBeenCalled()
  })

  it('calls posthog.reset after initialization', () => {
    process.env.NEXT_PUBLIC_POSTHOG_KEY = 'phc_test_key'
    const { initPostHog, resetUser } = getModule()
    initPostHog()
    resetUser()
    expect(mockReset).toHaveBeenCalled()
  })
})

describe('trackEvent', () => {
  it('does not call posthog.capture when not initialized', () => {
    const { trackEvent } = getModule()
    trackEvent('button_click', { label: 'deploy' })
    expect(mockCapture).not.toHaveBeenCalled()
  })

  it('calls posthog.capture after initialization', () => {
    process.env.NEXT_PUBLIC_POSTHOG_KEY = 'phc_test_key'
    const { initPostHog, trackEvent } = getModule()
    initPostHog()
    trackEvent('button_click', { label: 'deploy' })
    expect(mockCapture).toHaveBeenCalledWith('button_click', { label: 'deploy' })
  })
})
