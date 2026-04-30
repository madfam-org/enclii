/**
 * Tests for app/api/domains/route.ts
 *
 * Tests the GET and POST route handlers for the domains API,
 * which delegates to cloudflare-service functions.
 */

// Mock Next.js server runtime for route handlers
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
      const resp = new MockNextResponse(JSON.stringify(data), init)
      return resp
    }
  }

  return {
    NextResponse: MockNextResponse,
  }
})

// Mock the cloudflare-service module
jest.mock('@/lib/cloudflare-service', () => ({
  getDispatchDomains: jest.fn(),
  commissionDomain: jest.fn(),
}))

import { GET, POST } from '@/app/api/domains/route'
import { getDispatchDomains, commissionDomain } from '@/lib/cloudflare-service'

const mockGetDispatchDomains = getDispatchDomains as jest.MockedFunction<typeof getDispatchDomains>
const mockCommissionDomain = commissionDomain as jest.MockedFunction<typeof commissionDomain>

beforeEach(() => {
  jest.clearAllMocks()
})

// =============================================================================
// GET /api/domains
// =============================================================================

describe('GET /api/domains', () => {
  it('returns domains list on success', async () => {
    const domains = [
      { id: 'z1', domain: 'madfam.io', tenant: 'madfam' as const, status: 'active' as const },
    ]
    mockGetDispatchDomains.mockResolvedValueOnce(domains as any)

    const response = await GET()
    const body = await response.json()

    expect(body.success).toBe(true)
    expect(body.data).toEqual(domains)
  })

  it('returns error with 500 status on failure', async () => {
    mockGetDispatchDomains.mockRejectedValueOnce(new Error('Cloudflare API timeout'))

    const response = await GET()
    const body = await response.json()

    expect((response as any).status).toBe(500)
    expect(body.success).toBe(false)
    expect(body.error).toBe('Cloudflare API timeout')
  })

  it('returns generic error message for non-Error exceptions', async () => {
    mockGetDispatchDomains.mockRejectedValueOnce('unexpected string error')

    const response = await GET()
    const body = await response.json()

    expect((response as any).status).toBe(500)
    expect(body.success).toBe(false)
    expect(body.error).toBe('Failed to fetch domains')
  })
})

// =============================================================================
// POST /api/domains
// =============================================================================

describe('POST /api/domains', () => {
  function makeRequest(body: unknown): Request {
    return {
      json: () => Promise.resolve(body),
    } as unknown as Request
  }

  it('commissions a valid domain and returns result', async () => {
    const result = {
      zone: { id: 'z-new', name: 'newsite.dev', name_servers: ['ns1.cf', 'ns2.cf'] },
      nameservers: ['ns1.cf', 'ns2.cf'],
      instructions: ['Step 1', 'Step 2'],
    }
    mockCommissionDomain.mockResolvedValueOnce(result as any)

    const response = await POST(makeRequest({ domain: 'newsite.dev', tenant: 'other' }))
    const body = await response.json()

    expect(body.success).toBe(true)
    expect(body.data).toEqual(result)
  })

  it('returns 400 when domain is missing', async () => {
    const response = await POST(makeRequest({ tenant: 'other' }))
    const body = await response.json()

    expect((response as any).status).toBe(400)
    expect(body.success).toBe(false)
    expect(body.error).toBe('Domain is required')
  })

  it('returns 400 for invalid domain format', async () => {
    const response = await POST(makeRequest({ domain: '-invalid..com' }))
    const body = await response.json()

    expect((response as any).status).toBe(400)
    expect(body.success).toBe(false)
    expect(body.error).toBe('Invalid domain format')
  })
})
