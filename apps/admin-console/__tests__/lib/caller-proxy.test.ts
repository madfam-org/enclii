import { callerProxy } from '@/lib/caller-proxy'

// jsdom supplies Headers but not the server's Response constructor.
global.Response = class {
  status: number
  constructor(_body: string, init: ResponseInit) { this.status = init.status ?? 200 }
} as unknown as typeof Response
const mockFetch = jest.fn()
global.fetch = mockFetch
const originalEnv = process.env
beforeEach(() => {
  mockFetch.mockReset()
  process.env = { ...originalEnv, NEXT_PUBLIC_API_URL: 'https://api.example.org', SWITCHYARD_API_KEY: 'service-fixture' } // pragma: allowlist secret (synthetic authority fixture)
})
afterAll(() => { process.env = originalEnv })

it('refuses a missing caller even when a service key is configured', async () => {
  const response = await callerProxy('/ops/inventory/topology')
  expect(response.status).toBe(401)
  expect(mockFetch).not.toHaveBeenCalled()
})
it.each([401, 403])('never retries refusal %s with service authority', async (status) => {
  mockFetch.mockResolvedValue({ status })
  const response = await callerProxy('/ops/inventory/topology', { userToken: 'caller-fixture' })
  expect(response.status).toBe(status)
  expect(mockFetch).toHaveBeenCalledTimes(1)
  expect(mockFetch.mock.calls[0][1].headers.get('Authorization')).toBe('Bearer caller-fixture')
})
it('binds Authorization to the caller and disables caching', async () => {
  mockFetch.mockResolvedValue({ status: 200 })
  await callerProxy('/ops/inventory/topology', {
    userToken: 'caller-fixture', headers: { Authorization: 'Bearer injected-fixture' }, cache: 'force-cache',
  })
  const [url, options] = mockFetch.mock.calls[0]
  expect(url).toBe('https://api.example.org/v1/ops/inventory/topology')
  expect(options.headers.get('Authorization')).toBe('Bearer caller-fixture')
  expect(options.cache).toBe('no-store')
})
