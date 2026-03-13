/**
 * Tests for lib/admin-api.ts
 *
 * The admin-api module wraps fetch calls to /api/admin/* endpoints.
 * We mock global.fetch to verify correct URL construction, HTTP methods,
 * request bodies, and error handling.
 */

import {
  fleetApi,
  clusterApi,
  resourceApi,
  driftApi,
  costApi,
  topologyApi,
} from '@/lib/admin-api'

// ---------------------------------------------------------------------------
// Global fetch mock
// ---------------------------------------------------------------------------

const mockFetch = jest.fn()
global.fetch = mockFetch

function mockFetchSuccess(data: unknown) {
  mockFetch.mockResolvedValueOnce({
    ok: true,
    json: () => Promise.resolve(data),
  })
}

function mockFetchError(status: number, body: unknown = {}) {
  mockFetch.mockResolvedValueOnce({
    ok: false,
    status,
    json: () => Promise.resolve(body),
  })
}

beforeEach(() => {
  mockFetch.mockReset()
})

// =============================================================================
// fleetApi
// =============================================================================

describe('fleetApi', () => {
  it('list fetches GET /api/admin/fleet', async () => {
    const hosts = [{ id: 'h1', name: 'foundry-core' }]
    mockFetchSuccess({ hosts })

    const result = await fleetApi.list()

    expect(result).toEqual({ hosts })
    expect(mockFetch).toHaveBeenCalledWith('/api/admin/fleet', expect.objectContaining({
      headers: expect.objectContaining({ 'Content-Type': 'application/json' }),
    }))
  })

  it('get fetches GET /api/admin/fleet/:id', async () => {
    const host = { id: 'h1', name: 'foundry-core' }
    mockFetchSuccess(host)

    const result = await fleetApi.get('h1')

    expect(result).toEqual(host)
    expect(mockFetch).toHaveBeenCalledWith('/api/admin/fleet/h1', expect.any(Object))
  })

  it('register sends POST /api/admin/fleet with body', async () => {
    const newHost = { name: 'new-host', bmc_address: '10.0.0.1' }
    mockFetchSuccess({ ...newHost, id: 'h2' })

    await fleetApi.register(newHost)

    expect(mockFetch).toHaveBeenCalledWith('/api/admin/fleet', expect.objectContaining({
      method: 'POST',
      body: JSON.stringify(newHost),
    }))
  })

  it('power sends PUT /api/admin/fleet/:id/power', async () => {
    mockFetchSuccess({ status: 'ok' })

    await fleetApi.power('h1', 'off')

    expect(mockFetch).toHaveBeenCalledWith('/api/admin/fleet/h1/power', expect.objectContaining({
      method: 'PUT',
      body: JSON.stringify({ action: 'off' }),
    }))
  })

  it('wipe sends POST /api/admin/fleet/:id/wipe', async () => {
    mockFetchSuccess({ status: 'wiped' })

    await fleetApi.wipe('h1')

    expect(mockFetch).toHaveBeenCalledWith('/api/admin/fleet/h1/wipe', expect.objectContaining({
      method: 'POST',
    }))
  })
})

// =============================================================================
// clusterApi
// =============================================================================

describe('clusterApi', () => {
  it('list fetches GET /api/admin/clusters', async () => {
    const clusters = [{ id: 'c1', name: 'prod' }]
    mockFetchSuccess({ clusters })

    const result = await clusterApi.list()

    expect(result).toEqual({ clusters })
    expect(mockFetch).toHaveBeenCalledWith('/api/admin/clusters', expect.any(Object))
  })

  it('get fetches GET /api/admin/clusters/:id', async () => {
    mockFetchSuccess({ id: 'c1', name: 'prod' })

    const result = await clusterApi.get('c1')

    expect(result).toEqual({ id: 'c1', name: 'prod' })
  })

  it('register sends POST /api/admin/clusters', async () => {
    mockFetchSuccess({ id: 'c2', name: 'staging' })

    await clusterApi.register({ name: 'staging', type: 'k3s' } as any)

    expect(mockFetch).toHaveBeenCalledWith('/api/admin/clusters', expect.objectContaining({
      method: 'POST',
      body: JSON.stringify({ name: 'staging', type: 'k3s' }),
    }))
  })
})

// =============================================================================
// resourceApi
// =============================================================================

describe('resourceApi', () => {
  it('list without params fetches /api/admin/resources', async () => {
    mockFetchSuccess({ resources: [] })

    await resourceApi.list()

    expect(mockFetch).toHaveBeenCalledWith('/api/admin/resources', expect.any(Object))
  })

  it('list with params appends query string', async () => {
    mockFetchSuccess({ resources: [] })

    await resourceApi.list({ provider: 'hetzner', kind: 'server' })

    const calledUrl = mockFetch.mock.calls[0][0] as string
    expect(calledUrl).toContain('provider=hetzner')
    expect(calledUrl).toContain('kind=server')
  })
})

// =============================================================================
// driftApi
// =============================================================================

describe('driftApi', () => {
  it('list without filter fetches /api/admin/drift', async () => {
    mockFetchSuccess({ events: [] })

    await driftApi.list()

    expect(mockFetch).toHaveBeenCalledWith('/api/admin/drift', expect.any(Object))
  })

  it('list with resolved=false appends query string', async () => {
    mockFetchSuccess({ events: [] })

    await driftApi.list(false)

    const calledUrl = mockFetch.mock.calls[0][0] as string
    expect(calledUrl).toBe('/api/admin/drift?resolved=false')
  })

  it('list with resolved=true appends query string', async () => {
    mockFetchSuccess({ events: [] })

    await driftApi.list(true)

    const calledUrl = mockFetch.mock.calls[0][0] as string
    expect(calledUrl).toBe('/api/admin/drift?resolved=true')
  })
})

// =============================================================================
// costApi
// =============================================================================

describe('costApi', () => {
  it('list without params fetches /api/admin/costs', async () => {
    mockFetchSuccess({ allocations: [] })

    await costApi.list()

    expect(mockFetch).toHaveBeenCalledWith('/api/admin/costs', expect.any(Object))
  })

  it('summary appends query parameters', async () => {
    mockFetchSuccess({ summary: [] })

    await costApi.summary({ start: '2026-01-01', end: '2026-01-31' })

    const calledUrl = mockFetch.mock.calls[0][0] as string
    expect(calledUrl).toContain('start=2026-01-01')
    expect(calledUrl).toContain('end=2026-01-31')
  })
})

// =============================================================================
// topologyApi
// =============================================================================

describe('topologyApi', () => {
  it('get fetches /api/admin/topology', async () => {
    const topo = { nodes: [], edges: [] }
    mockFetchSuccess(topo)

    const result = await topologyApi.get()

    expect(result).toEqual(topo)
    expect(mockFetch).toHaveBeenCalledWith('/api/admin/topology', expect.any(Object))
  })
})

// =============================================================================
// Error handling
// =============================================================================

describe('adminFetch error handling', () => {
  it('throws with error message from response body', async () => {
    mockFetchError(403, { error: 'Forbidden: insufficient role' })

    await expect(fleetApi.list()).rejects.toThrow('Forbidden: insufficient role')
  })

  it('throws with status code when body has no error field', async () => {
    mockFetchError(500, {})

    await expect(fleetApi.list()).rejects.toThrow('API error: 500')
  })

  it('throws with status code when JSON parsing fails', async () => {
    mockFetch.mockResolvedValueOnce({
      ok: false,
      status: 502,
      json: () => Promise.reject(new Error('invalid json')),
    })

    await expect(fleetApi.list()).rejects.toThrow('API error: 502')
  })
})
