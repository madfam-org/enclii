/**
 * Tests for lib/cloudflare-service.ts
 *
 * Tests the Cloudflare API wrapper functions including zone management,
 * DNS record operations, tenant extraction, and the unified domain list.
 */

// We need to set env vars BEFORE importing the module so getCredentials() works
const originalEnv = process.env
beforeEach(() => {
  process.env = {
    ...originalEnv,
    CLOUDFLARE_API_TOKEN: 'test-cf-token',
    CLOUDFLARE_ACCOUNT_ID: 'test-account-id',
  }
  mockFetch.mockReset()
})

afterAll(() => {
  process.env = originalEnv
})

const mockFetch = jest.fn()
global.fetch = mockFetch

// Helper to create a successful Cloudflare API response
function cfSuccess<T>(result: T) {
  return {
    ok: true,
    json: () => Promise.resolve({ success: true, result, errors: [], messages: [] }),
  }
}

// Helper to create a failed Cloudflare API response
function cfError(message: string) {
  return {
    ok: true,
    json: () => Promise.resolve({
      success: false,
      result: null,
      errors: [{ code: 1000, message }],
      messages: [],
    }),
  }
}

// Import AFTER mocking fetch and setting env
import {
  listZones,
  createZone,
  deleteZone,
  listDNSRecords,
  createDNSRecord,
  getDispatchDomains,
  commissionDomain,
} from '@/lib/cloudflare-service'

// =============================================================================
// getCredentials (tested indirectly via API calls)
// =============================================================================

describe('getCredentials', () => {
  it('throws when CLOUDFLARE_API_TOKEN is missing', async () => {
    delete process.env.CLOUDFLARE_API_TOKEN

    await expect(listZones()).rejects.toThrow('CLOUDFLARE_API_TOKEN is not configured')
  })

  it('throws when CLOUDFLARE_ACCOUNT_ID is missing for zone creation', async () => {
    delete process.env.CLOUDFLARE_ACCOUNT_ID

    await expect(createZone('example.com')).rejects.toThrow('CLOUDFLARE_ACCOUNT_ID is not configured')
  })
})

// =============================================================================
// listZones
// =============================================================================

describe('listZones', () => {
  it('fetches /zones with correct auth header', async () => {
    const zones = [{ id: 'z1', name: 'madfam.io', status: 'active' }]
    mockFetch.mockResolvedValueOnce(cfSuccess(zones))

    const result = await listZones()

    expect(result.result).toEqual(zones)
    expect(mockFetch).toHaveBeenCalledWith(
      'https://api.cloudflare.com/client/v4/zones',
      expect.objectContaining({
        headers: expect.objectContaining({
          Authorization: 'Bearer test-cf-token',
          'Content-Type': 'application/json',
        }),
      })
    )
  })

  it('includes query parameters when provided', async () => {
    mockFetch.mockResolvedValueOnce(cfSuccess([]))

    await listZones({ name: 'madfam.io', status: 'active', perPage: 50 })

    const calledUrl = mockFetch.mock.calls[0][0] as string
    expect(calledUrl).toContain('name=madfam.io')
    expect(calledUrl).toContain('status=active')
    expect(calledUrl).toContain('per_page=50')
  })
})

// =============================================================================
// createZone
// =============================================================================

describe('createZone', () => {
  it('sends POST /zones with account ID and domain', async () => {
    const zone = { id: 'z1', name: 'newdomain.dev', name_servers: ['ns1.cf', 'ns2.cf'] }
    mockFetch.mockResolvedValueOnce(cfSuccess(zone))

    const result = await createZone('newdomain.dev')

    expect(result).toEqual(zone)
    const body = JSON.parse(mockFetch.mock.calls[0][1].body)
    expect(body).toEqual({
      name: 'newdomain.dev',
      account: { id: 'test-account-id' },
      jump_start: true,
      type: 'full',
    })
  })

  it('passes custom options through', async () => {
    mockFetch.mockResolvedValueOnce(cfSuccess({ id: 'z2', name: 'test.com', name_servers: [] }))

    await createZone('test.com', { jumpStart: false, type: 'partial' })

    const body = JSON.parse(mockFetch.mock.calls[0][1].body)
    expect(body.jump_start).toBe(false)
    expect(body.type).toBe('partial')
  })
})

// =============================================================================
// deleteZone
// =============================================================================

describe('deleteZone', () => {
  it('sends DELETE /zones/:id', async () => {
    mockFetch.mockResolvedValueOnce(cfSuccess({ id: 'z1' }))

    const result = await deleteZone('z1')

    expect(result).toEqual({ id: 'z1' })
    expect(mockFetch).toHaveBeenCalledWith(
      'https://api.cloudflare.com/client/v4/zones/z1',
      expect.objectContaining({ method: 'DELETE' })
    )
  })
})

// =============================================================================
// listDNSRecords
// =============================================================================

describe('listDNSRecords', () => {
  it('fetches DNS records for the given zone ID', async () => {
    const records = [{ id: 'r1', type: 'CNAME', name: 'api.madfam.io' }]
    mockFetch.mockResolvedValueOnce(cfSuccess(records))

    const result = await listDNSRecords('zone-123')

    expect(result.result).toEqual(records)
    expect(mockFetch).toHaveBeenCalledWith(
      'https://api.cloudflare.com/client/v4/zones/zone-123/dns_records',
      expect.any(Object)
    )
  })

  it('appends filter params to the DNS records URL', async () => {
    mockFetch.mockResolvedValueOnce(cfSuccess([]))

    await listDNSRecords('zone-123', { type: 'CNAME', name: 'api' })

    const calledUrl = mockFetch.mock.calls[0][0] as string
    expect(calledUrl).toContain('type=CNAME')
    expect(calledUrl).toContain('name=api')
  })
})

// =============================================================================
// createDNSRecord
// =============================================================================

describe('createDNSRecord', () => {
  it('sends POST with default TTL and proxied values', async () => {
    const record = { id: 'r1', name: 'test', type: 'CNAME', content: 'target.com' }
    mockFetch.mockResolvedValueOnce(cfSuccess(record))

    await createDNSRecord('zone-123', {
      type: 'CNAME',
      name: 'test',
      content: 'target.com',
    })

    const body = JSON.parse(mockFetch.mock.calls[0][1].body)
    expect(body.ttl).toBe(1) // automatic
    expect(body.proxied).toBe(true)
  })

  it('allows overriding TTL and proxied', async () => {
    mockFetch.mockResolvedValueOnce(cfSuccess({ id: 'r2' }))

    await createDNSRecord('zone-123', {
      type: 'A',
      name: '@',
      content: '1.2.3.4',
      ttl: 3600,
      proxied: false,
    })

    const body = JSON.parse(mockFetch.mock.calls[0][1].body)
    expect(body.ttl).toBe(3600)
    expect(body.proxied).toBe(false)
  })
})

// =============================================================================
// getDispatchDomains (also tests extractTenant indirectly)
// =============================================================================

describe('getDispatchDomains', () => {
  it('maps zones to DispatchDomain objects with correct tenant extraction', async () => {
    const zones = [
      {
        id: 'z1',
        name: 'madfam.io',
        status: 'active',
        name_servers: ['ns1.cf', 'ns2.cf'],
        activated_on: '2025-01-01T00:00:00Z',
        created_on: '2025-01-01T00:00:00Z',
      },
      {
        id: 'z2',
        name: 'enclii.dev',
        status: 'active',
        name_servers: ['ns1.cf', 'ns2.cf'],
        activated_on: '2025-02-01T00:00:00Z',
        created_on: '2025-02-01T00:00:00Z',
      },
      {
        id: 'z3',
        name: 'unknown-domain.com',
        status: 'pending',
        name_servers: ['ns1.cf', 'ns2.cf'],
        activated_on: null,
        created_on: '2025-03-01T00:00:00Z',
      },
    ]

    // listZones call
    mockFetch.mockResolvedValueOnce(cfSuccess(zones))
    // listTunnels call
    mockFetch.mockResolvedValueOnce(cfSuccess([]))

    const domains = await getDispatchDomains()

    expect(domains).toHaveLength(3)
    expect(domains[0].tenant).toBe('madfam')
    expect(domains[0].sslStatus).toBe('active')
    expect(domains[1].tenant).toBe('enclii')
    expect(domains[2].tenant).toBe('other')
    expect(domains[2].sslStatus).toBe('pending')
  })
})

// =============================================================================
// commissionDomain
// =============================================================================

describe('commissionDomain', () => {
  it('creates a zone and returns instructions with nameservers', async () => {
    const zone = {
      id: 'z-new',
      name: 'newdomain.dev',
      name_servers: ['ada.ns.cloudflare.com', 'bob.ns.cloudflare.com'],
      status: 'pending',
    }
    mockFetch.mockResolvedValueOnce(cfSuccess(zone))

    const result = await commissionDomain({ domain: 'newdomain.dev', tenant: 'other' })

    expect(result.zone).toEqual(zone)
    expect(result.nameservers).toEqual(['ada.ns.cloudflare.com', 'bob.ns.cloudflare.com'])
    // Instructions include 5 base steps + 1 line per nameserver (spread via ...map)
    // Total: 5 base lines + 2 nameserver lines = 7
    expect(result.instructions.length).toBeGreaterThanOrEqual(5)
    expect(result.instructions[0]).toContain('registrar')
    expect(result.instructions[2]).toContain('nameservers')
    // Verify nameservers are embedded in instructions
    const allInstructions = result.instructions.join('\n')
    expect(allInstructions).toContain('ada.ns.cloudflare.com')
    expect(allInstructions).toContain('bob.ns.cloudflare.com')
  })
})

// =============================================================================
// Cloudflare API error handling
// =============================================================================

describe('Cloudflare API error handling', () => {
  it('throws with Cloudflare error message on API failure', async () => {
    mockFetch.mockResolvedValueOnce(cfError('Zone already exists'))

    await expect(listZones()).rejects.toThrow('Cloudflare API Error: Zone already exists')
  })
})
