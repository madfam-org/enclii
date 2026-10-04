/** @jest-environment node */
import { adminProxy } from '@/lib/admin-proxy'
import { switchyardProxy } from '@/lib/switchyard-proxy'

const mockFetch = jest.fn()
global.fetch = mockFetch
const originalEnv = process.env

beforeEach(() => {
  mockFetch.mockReset()
  process.env = { ...originalEnv, NEXT_PUBLIC_API_URL: 'https://api.example.org', SWITCHYARD_API_KEY: 'service-key-must-not-be-used' }
})
afterAll(() => { process.env = originalEnv })

const proxies = [
  { name: 'admin', call: (options?: RequestInit & { userToken?: string }) => adminProxy('/fleet', options), path: '/v1/admin/fleet' },
  { name: 'ops', call: (options?: RequestInit & { userToken?: string }) => switchyardProxy('ops', '/apps/sync', options), path: '/v1/ops/apps/sync' },
  { name: 'providers', call: (options?: RequestInit & { userToken?: string }) => switchyardProxy('providers', '/cloudflare/zones', options), path: '/v1/providers/cloudflare/zones' },
]

describe.each(proxies)('$name caller identity', ({ call, path }) => {
  it('forwards the caller and options without allowing header identity replacement', async () => {
    const upstream = new Response('{}', { status: 200 })
    mockFetch.mockResolvedValueOnce(upstream)
    expect(await call({ userToken: 'caller-jwt', method: 'POST', body: '{}', headers: { Authorization: 'Bearer other-principal', 'X-Test': 'kept' } })).toBe(upstream)
    expect(mockFetch).toHaveBeenCalledTimes(1)
    const [url, options] = mockFetch.mock.calls[0]
    expect(url).toBe(`https://api.example.org${path}`)
    expect(options).toMatchObject({ method: 'POST', body: '{}', cache: 'no-store' })
    expect(options.headers.get('Authorization')).toBe('Bearer caller-jwt')
    expect(options.headers.get('X-Test')).toBe('kept')
  })

  it.each([401, 403])('preserves upstream %s without retrying as a service principal', async (status) => {
    const upstream = new Response('{}', { status })
    mockFetch.mockResolvedValueOnce(upstream)
    expect(await call({ userToken: 'rejected-caller' })).toBe(upstream)
    expect(mockFetch).toHaveBeenCalledTimes(1)
  })

  it('rejects missing caller credentials without contacting Switchyard', async () => {
    const result = await call({ headers: { Authorization: 'Bearer other-principal' } })
    expect(result.status).toBe(401)
    expect(mockFetch).not.toHaveBeenCalled()
  })
})
