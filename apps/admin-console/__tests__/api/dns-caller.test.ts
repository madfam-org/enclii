/** @jest-environment node */
import { GET, POST } from '@/app/api/domains/[zoneId]/dns/route'
import { switchyardProviderCall } from '@/lib/switchyard-proxy'

jest.mock('next/headers', () => ({
  cookies: jest.fn(async () => ({ get: (name: string) => name === 'dispatch_auth' ? { value: 'caller-jwt' } : undefined })),
}))
jest.mock('@/lib/switchyard-proxy', () => ({ switchyardProviderCall: jest.fn() }))
const mockCall = jest.mocked(switchyardProviderCall)
const params = { params: Promise.resolve({ zoneId: 'fixture-zone' }) }

beforeEach(() => jest.clearAllMocks())

it('reads DNS records as the operator, not the service key', async () => {
  mockCall.mockResolvedValue({ ok: true, status: 200, data: { data: { records: [] } } })
  expect((await GET(new Request('https://console.example.org'), params)).status).toBe(200)
  expect(mockCall).toHaveBeenCalledWith('cloudflare', 'dns', {
    dry_run: true, args: { zone_id: 'fixture-zone' },
  }, 'caller-jwt')
})

it('retains the caller for DNS writes and preserves a platform-rank refusal', async () => {
  mockCall.mockResolvedValue({ ok: false, status: 403, data: { summary: 'Forbidden' } })
  const request = new Request('https://console.example.org', {
    method: 'POST', body: JSON.stringify({ type: 'TXT', name: 'fixture.example.org', content: 'fixture', reason: 'Test DNS intent' }),
  })
  expect((await POST(request, params)).status).toBe(403)
  expect(mockCall).toHaveBeenCalledWith('cloudflare', 'dns-apply', expect.objectContaining({
    dry_run: false, reason: 'Test DNS intent',
  }), 'caller-jwt')
})
