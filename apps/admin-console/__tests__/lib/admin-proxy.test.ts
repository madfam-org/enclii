/**
 * Tests for lib/admin-proxy.ts
 *
 * The admin-proxy module proxies requests to the Switchyard API with
 * a resilient auth fallback strategy:
 *   1. Try user JWT
 *   2. If 401 and API key available, retry with API key
 *   3. If no user JWT, use API key (or unauthenticated)
 */

import { adminProxy } from '@/lib/admin-proxy'

// ---------------------------------------------------------------------------
// Global fetch mock
// ---------------------------------------------------------------------------

const mockFetch = jest.fn()
global.fetch = mockFetch

const originalEnv = process.env

beforeEach(() => {
  mockFetch.mockReset()
  process.env = { ...originalEnv }
  // Set defaults for tests
  process.env.NEXT_PUBLIC_API_URL = 'https://api.enclii.dev'
  process.env.SWITCHYARD_API_KEY = 'sk-test-key'
})

afterAll(() => {
  process.env = originalEnv
})

describe('adminProxy', () => {
  it('uses user token as Bearer when provided', async () => {
    mockFetch.mockResolvedValueOnce({ status: 200, ok: true })

    await adminProxy('/fleet', { userToken: 'user-jwt-token' })

    expect(mockFetch).toHaveBeenCalledTimes(1)
    expect(mockFetch).toHaveBeenCalledWith(
      'https://api.enclii.dev/v1/admin/fleet',
      expect.objectContaining({
        headers: expect.objectContaining({
          Authorization: 'Bearer user-jwt-token',
          'Content-Type': 'application/json',
        }),
      })
    )
  })

  it('falls back to API key when user token gets 401', async () => {
    // First call with user token returns 401
    mockFetch.mockResolvedValueOnce({ status: 401, ok: false })
    // Second call with API key returns 200
    mockFetch.mockResolvedValueOnce({ status: 200, ok: true })

    await adminProxy('/fleet', { userToken: 'expired-jwt' })

    expect(mockFetch).toHaveBeenCalledTimes(2)
    // Second call should use the API key
    expect(mockFetch.mock.calls[1][1]).toEqual(
      expect.objectContaining({
        headers: expect.objectContaining({
          Authorization: 'Bearer sk-test-key',
        }),
      })
    )
  })

  it('uses API key directly when no user token is provided', async () => {
    mockFetch.mockResolvedValueOnce({ status: 200, ok: true })

    await adminProxy('/clusters')

    expect(mockFetch).toHaveBeenCalledTimes(1)
    expect(mockFetch).toHaveBeenCalledWith(
      'https://api.enclii.dev/v1/admin/clusters',
      expect.objectContaining({
        headers: expect.objectContaining({
          Authorization: 'Bearer sk-test-key',
        }),
      })
    )
  })

  it('sends unauthenticated request when neither token nor API key available', async () => {
    delete process.env.SWITCHYARD_API_KEY
    mockFetch.mockResolvedValueOnce({ status: 200, ok: true })

    await adminProxy('/topology')

    expect(mockFetch).toHaveBeenCalledTimes(1)
    const headers = mockFetch.mock.calls[0][1].headers
    expect(headers).not.toHaveProperty('Authorization')
  })

  it('constructs URL using NEXT_PUBLIC_API_URL and /v1/admin prefix', async () => {
    process.env.NEXT_PUBLIC_API_URL = 'https://custom-api.example.com'
    mockFetch.mockResolvedValueOnce({ status: 200, ok: true })

    await adminProxy('/drift')

    expect(mockFetch).toHaveBeenCalledWith(
      'https://custom-api.example.com/v1/admin/drift',
      expect.any(Object)
    )
  })

  it('passes through additional request options', async () => {
    mockFetch.mockResolvedValueOnce({ status: 201, ok: true })

    await adminProxy('/fleet', {
      method: 'POST',
      body: JSON.stringify({ name: 'test-host' }),
    })

    expect(mockFetch).toHaveBeenCalledWith(
      'https://api.enclii.dev/v1/admin/fleet',
      expect.objectContaining({
        method: 'POST',
        body: JSON.stringify({ name: 'test-host' }),
      })
    )
  })
})
