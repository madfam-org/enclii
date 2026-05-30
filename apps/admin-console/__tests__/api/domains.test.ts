/**
 * Tests for app/api/domains/route.ts
 *
 * Domains API delegates to Switchyard Cloudflare provider via switchyard-proxy.
 */

jest.mock('next/server', () => {
  class MockNextResponse {
    body: unknown
    status: number
    headers: Map<string, string>

    constructor(body: string, init?: { status?: number; headers?: Record<string, string> }) {
      this.body = JSON.parse(body)
      this.status = init?.status || 200
      this.headers = new Map(Object.entries(init?.headers || {}))
    }

    async json() {
      return this.body
    }

    static json(data: unknown, init?: { status?: number }) {
      return new MockNextResponse(JSON.stringify(data), init)
    }
  }

  return { NextResponse: MockNextResponse }
})

jest.mock('@/lib/switchyard-proxy', () => ({
  switchyardProviderCall: jest.fn(),
}))

import { GET, POST } from '@/app/api/domains/route'
import { switchyardProviderCall } from '@/lib/switchyard-proxy'

const mockProviderCall = switchyardProviderCall as jest.MockedFunction<typeof switchyardProviderCall>

beforeEach(() => {
  jest.clearAllMocks()
})

describe('GET /api/domains', () => {
  it('returns mapped domains on success', async () => {
    mockProviderCall.mockResolvedValueOnce({
      ok: true,
      status: 200,
      data: {
        data: {
          zones: [
            {
              id: 'z1',
              name: 'madfam.io',
              status: 'active',
              name_servers: ['ns1.cf', 'ns2.cf'],
              activated_on: '2026-01-01T00:00:00Z',
              created_on: '2025-12-01T00:00:00Z',
            },
          ],
        },
      },
    })

    const response = await GET()
    const body = await response.json()

    expect(mockProviderCall).toHaveBeenCalledWith('cloudflare', 'zones', { dry_run: true })
    expect(body.success).toBe(true)
    expect(body.data).toEqual([
      expect.objectContaining({
        id: 'z1',
        domain: 'madfam.io',
        tenant: 'madfam',
        status: 'active',
        nameservers: ['ns1.cf', 'ns2.cf'],
      }),
    ])
  })

  it('returns 502 when Switchyard provider call fails', async () => {
    mockProviderCall.mockResolvedValueOnce({
      ok: false,
      status: 502,
      data: { summary: 'Cloudflare API timeout' },
    })

    const response = await GET()
    const body = await response.json()

    expect((response as { status: number }).status).toBe(502)
    expect(body.success).toBe(false)
    expect(body.error).toBe('Cloudflare API timeout')
  })

  it('returns 500 when proxy throws', async () => {
    mockProviderCall.mockRejectedValueOnce(new Error('network down'))

    const response = await GET()
    const body = await response.json()

    expect((response as { status: number }).status).toBe(500)
    expect(body.success).toBe(false)
    expect(body.error).toBe('network down')
  })
})

describe('POST /api/domains', () => {
  function makeRequest(body: unknown): Request {
    return {
      json: () => Promise.resolve(body),
    } as unknown as Request
  }

  it('commissions a valid domain and returns result', async () => {
    mockProviderCall.mockResolvedValueOnce({
      ok: true,
      status: 200,
      data: {
        data: {
          zone: {
            id: 'z-new',
            name: 'newsite.dev',
            status: 'pending',
            name_servers: ['ns1.cf', 'ns2.cf'],
          },
          nameservers: ['ns1.cf', 'ns2.cf'],
        },
      },
    })

    const response = await POST(makeRequest({ domain: 'newsite.dev', tenant: 'other' }))
    const body = await response.json()

    expect(mockProviderCall).toHaveBeenCalledWith('cloudflare', 'zone-add-apply', {
      dry_run: false,
      reason: 'Commission domain newsite.dev via Dispatch',
      args: { target: 'newsite.dev' },
    })
    expect(body.success).toBe(true)
    expect(body.data.nameservers).toEqual(['ns1.cf', 'ns2.cf'])
    expect(body.data.instructions).toEqual(
      expect.arrayContaining([expect.stringContaining('newsite.dev')])
    )
  })

  it('returns 400 when domain is missing', async () => {
    const response = await POST(makeRequest({ tenant: 'other' }))
    const body = await response.json()

    expect((response as { status: number }).status).toBe(400)
    expect(body.success).toBe(false)
    expect(body.error).toBe('Domain is required')
  })

  it('returns 400 for invalid domain format', async () => {
    const response = await POST(makeRequest({ domain: '-invalid..com' }))
    const body = await response.json()

    expect((response as { status: number }).status).toBe(400)
    expect(body.success).toBe(false)
    expect(body.error).toBe('Invalid domain format')
  })
})
