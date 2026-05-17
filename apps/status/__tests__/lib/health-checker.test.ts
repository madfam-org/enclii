/**
 * Health Checker Tests
 *
 * Mock global fetch and config module to test health-checker.ts
 * in isolation from network and environment.
 */

// Mock the config module before importing health-checker
jest.mock('@/lib/config', () => ({
  getHealthCheckTimeout: () => 5000,
  getCacheTTL: () => 30,
  getRetryCount: () => 2,
  getRetryDelayMs: () => 10, // very short for test speed
}))

import { checkService, checkAllServices, clearCache, getCacheStats } from '@/lib/health-checker'
import type { ServiceConfig } from '@/lib/types'

// Store the original fetch
const originalFetch = global.fetch

// Helper to build a ServiceConfig
function makeService(overrides?: Partial<ServiceConfig>): ServiceConfig {
  return {
    name: 'Test API',
    url: 'https://api.test.com/health',
    group: 'Test',
    ...overrides,
  }
}

// Helper to create a mock Response-like object (jsdom lacks native Response).
// Optional `body` opts in to the assertion path. `body` is exposed via a
// `text()` thunk to mirror the production fetch Response shape used by
// `readBodyCapped`'s fallback branch.
function fakeResponse(
  status: number,
  body?: string,
  url = 'https://api.test.com/health'
): { status: number; ok: boolean; url: string; text?: () => Promise<string>; body?: null } {
  const base = { status, ok: status >= 200 && status < 300, url }
  if (body === undefined) return base
  return {
    ...base,
    body: null, // forces readBodyCapped() to use the response.text() fallback
    text: () => Promise.resolve(body),
  }
}

beforeEach(() => {
  clearCache()
  jest.restoreAllMocks()
})

afterAll(() => {
  global.fetch = originalFetch
})

describe('checkService', () => {
  it('returns operational for 200 response', async () => {
    global.fetch = jest.fn().mockResolvedValue(fakeResponse(200))
    const result = await checkService(makeService())

    expect(result.status).toBe('operational')
    expect(result.service).toBe('Test API')
    expect(result.group).toBe('Test')
    expect(result.url).toBe('https://api.test.com/health')
    expect(result.responseTime).toBeGreaterThanOrEqual(0)
    expect(result.statusCode).toBe(200)
    expect(result.lastChecked).toBeDefined()
  })

  it('returns operational for other 2xx responses', async () => {
    global.fetch = jest.fn().mockResolvedValue(fakeResponse(201))
    const result = await checkService(makeService())
    expect(result.status).toBe('operational')
    expect(result.statusCode).toBe(201)
  })

  it('returns degraded for 404', async () => {
    global.fetch = jest.fn().mockResolvedValue(fakeResponse(404))
    const result = await checkService(makeService())
    expect(result.status).toBe('degraded')
    expect(result.statusCode).toBe(404)
  })

  it('returns maintenance for 503', async () => {
    global.fetch = jest.fn().mockResolvedValue(fakeResponse(503))
    const result = await checkService(makeService())
    expect(result.status).toBe('maintenance')
    expect(result.statusCode).toBe(503)
  })

  it('returns outage for 500 (after retries)', async () => {
    // 500 is retryable, so it will be tried 1 + 2 retries = 3 times total
    global.fetch = jest.fn().mockResolvedValue(fakeResponse(500))
    const result = await checkService(makeService())
    expect(result.status).toBe('outage')
    expect(result.statusCode).toBe(500)
    // Initial attempt + 2 retries = 3 calls
    expect(global.fetch).toHaveBeenCalledTimes(3)
  })

  it('returns outage on timeout (AbortError)', async () => {
    const abortError = new DOMException('The operation was aborted', 'AbortError')
    global.fetch = jest.fn().mockRejectedValue(abortError)
    const result = await checkService(makeService())
    expect(result.status).toBe('outage')
    expect(result.error).toBe('Request timed out')
  })

  it('returns outage on connection refused', async () => {
    global.fetch = jest.fn().mockRejectedValue(new Error('connect ECONNREFUSED 127.0.0.1:443'))
    const result = await checkService(makeService())
    expect(result.status).toBe('outage')
    expect(result.error).toBe('Connection refused')
  })

  it('returns outage on DNS failure', async () => {
    global.fetch = jest.fn().mockRejectedValue(new Error('getaddrinfo ENOTFOUND api.test.com'))
    const result = await checkService(makeService())
    expect(result.status).toBe('outage')
    expect(result.error).toBe('DNS lookup failed')
  })

  it('retries on 5xx and succeeds on second try', async () => {
    const fetchMock = jest.fn()
      .mockResolvedValueOnce(fakeResponse(502))
      .mockResolvedValueOnce(fakeResponse(200))

    global.fetch = fetchMock
    const result = await checkService(makeService())
    expect(result.status).toBe('operational')
    expect(result.statusCode).toBe(200)
    // Initial 502 + 1 retry succeeds
    expect(fetchMock).toHaveBeenCalledTimes(2)
  })

  it('retries on timeout and gives up after max retries', async () => {
    const abortError = new DOMException('The operation was aborted', 'AbortError')
    global.fetch = jest.fn().mockRejectedValue(abortError)
    const result = await checkService(makeService())
    expect(result.status).toBe('outage')
    expect(result.error).toBe('Request timed out')
    // Initial + 2 retries = 3 total
    expect(global.fetch).toHaveBeenCalledTimes(3)
  })

  it('does not retry on 4xx (not retryable)', async () => {
    global.fetch = jest.fn().mockResolvedValue(fakeResponse(403))
    const result = await checkService(makeService())
    expect(result.status).toBe('degraded')
    // Only 1 call -- no retries for 4xx
    expect(global.fetch).toHaveBeenCalledTimes(1)
  })

  it('does not retry on 503 (maintenance, not retryable)', async () => {
    global.fetch = jest.fn().mockResolvedValue(fakeResponse(503))
    const result = await checkService(makeService())
    expect(result.status).toBe('maintenance')
    // Only 1 call -- 503 is maintenance, not retryable
    expect(global.fetch).toHaveBeenCalledTimes(1)
  })

  it('caches results within TTL', async () => {
    global.fetch = jest.fn().mockResolvedValue(fakeResponse(200))

    const service = makeService()
    const result1 = await checkService(service)
    const result2 = await checkService(service)

    expect(result1.status).toBe('operational')
    expect(result2.status).toBe('operational')
    // Only 1 fetch call -- second request served from cache
    expect(global.fetch).toHaveBeenCalledTimes(1)
  })

  it('refreshes cache after TTL expires', async () => {
    // Strategy: use fetch mock call count to switch Date.now phase.
    // After the first fetch call completes, we know the first checkService
    // is finishing. All subsequent Date.now calls should return expired time.
    let fetchCallCount = 0
    const fetchMock = jest.fn().mockImplementation(() => {
      fetchCallCount++
      return Promise.resolve(fakeResponse(200))
    })
    global.fetch = fetchMock

    // TTL is 30 seconds (30000ms). First phase returns t=1000,
    // second phase returns t=32000 (past expiry of 1000+30000=31000).
    jest.spyOn(Date, 'now').mockImplementation(() => {
      // Once the first fetch has been called, the first checkService
      // is processing. After that completes and the second starts,
      // fetchCallCount is 1 and Date.now should return expired time.
      // The key insight: the cache expiry check happens BEFORE fetch.
      // So: first checkService starts (Date.now=1000), calls fetch (count goes to 1),
      // finishes (cache stored with expiresAt=31000).
      // Second checkService starts, Date.now needs to be > 31000 to skip cache.
      // At that point fetchCallCount is already 1.
      return fetchCallCount >= 1 ? 32000 : 1000
    })

    const service = makeService()
    await checkService(service)
    await checkService(service)

    // Both calls should have fetched because cache expired
    expect(fetchMock).toHaveBeenCalledTimes(2)

    jest.spyOn(Date, 'now').mockRestore()
  })

  it('includes href from service config when provided', async () => {
    global.fetch = jest.fn().mockResolvedValue(fakeResponse(200))

    const service = makeService({
      href: 'https://api.test.com',
      url: 'https://api.test.com/health',
    })
    const result = await checkService(service)

    expect(result.href).toBe('https://api.test.com')
    expect(result.url).toBe('https://api.test.com/health')
  })

  it('does not include href when not in service config', async () => {
    global.fetch = jest.fn().mockResolvedValue(fakeResponse(200))

    const service = makeService() // no href
    const result = await checkService(service)

    expect(result.href).toBeUndefined()
  })

  it('includes description from service config', async () => {
    global.fetch = jest.fn().mockResolvedValue(fakeResponse(200))

    const service = makeService({ description: 'Control plane API' })
    const result = await checkService(service)

    expect(result.description).toBe('Control plane API')
  })

  it('uses assertion options in the cache key', async () => {
    global.fetch = jest
      .fn()
      .mockResolvedValueOnce(fakeResponse(200, 'MADFAM login', 'https://crm.madfam.io/login'))
      .mockResolvedValueOnce(fakeResponse(200, 'Generic login', 'https://crm.phynd.app/login'))

    await checkService(makeService({
      url: 'https://crm.test/login',
      assertContains: 'MADFAM login',
    }))
    await checkService(makeService({
      url: 'https://crm.test/login',
      assertContains: 'Generic login',
    }))

    expect(global.fetch).toHaveBeenCalledTimes(2)
  })
})

describe('checkService — content-match assertions', () => {
  it('assertContains pass: 200 + body has the marker → operational', async () => {
    global.fetch = jest
      .fn()
      .mockResolvedValue(fakeResponse(200, '<html>Karafiel marketplace v3.2.0</html>'))

    const result = await checkService(
      makeService({ assertContains: 'Karafiel marketplace' })
    )

    expect(result.status).toBe('operational')
    expect(result.statusCode).toBe(200)
    expect(result.error).toBeUndefined()
  })

  it('assertContains fail: 200 + body missing the marker → degraded', async () => {
    global.fetch = jest
      .fn()
      .mockResolvedValue(fakeResponse(200, '<html>React Router default scaffold</html>'))

    const result = await checkService(
      makeService({ assertContains: 'Karafiel marketplace' })
    )

    expect(result.status).toBe('degraded')
    expect(result.statusCode).toBe(200)
    expect(result.error).toBe('body missing required content')
  })

  it('assertNotContains pass: 200 + body lacks the forbidden token → operational', async () => {
    global.fetch = jest
      .fn()
      .mockResolvedValue(fakeResponse(200, 'apiBase=https://api.almanac.solar'))

    const result = await checkService(
      makeService({ assertNotContains: 'localhost:' })
    )

    expect(result.status).toBe('operational')
    expect(result.error).toBeUndefined()
  })

  it('assertNotContains fail: 200 + body has the forbidden token → degraded', async () => {
    global.fetch = jest
      .fn()
      .mockResolvedValue(fakeResponse(200, 'apiBase=http://localhost:8000'))

    const result = await checkService(
      makeService({ assertNotContains: 'localhost:' })
    )

    expect(result.status).toBe('degraded')
    expect(result.statusCode).toBe(200)
    expect(result.error).toBe('body contains forbidden content')
  })

  it('does not read the body when no assertion is configured (legacy fast-path)', async () => {
    // text() throwing would surface as a degraded body-read error; the fact
    // that the result is operational proves text() was never invoked.
    const text = jest.fn(() => {
      throw new Error('text() should not be called when no assertion is set')
    })
    global.fetch = jest.fn().mockResolvedValue({
      status: 200,
      ok: true,
      body: null,
      text,
    })

    const result = await checkService(makeService())

    expect(result.status).toBe('operational')
    expect(text).not.toHaveBeenCalled()
  })

  it('honors probeUrl override while preserving url for display', async () => {
    const fetchMock = jest.fn().mockResolvedValue(fakeResponse(200))
    global.fetch = fetchMock

    const result = await checkService(
      makeService({
        url: 'https://forgesight.quest',
        probeUrl: 'https://forgesight.quest/health',
      })
    )

    expect(result.status).toBe('operational')
    expect(result.url).toBe('https://forgesight.quest')
    // Probe should hit the override, not the user-facing URL.
    expect(fetchMock).toHaveBeenCalledWith(
      'https://forgesight.quest/health',
      expect.any(Object)
    )
  })
})

describe('checkService — final URL assertions', () => {
  it('assertFinalUrlContains pass: 200 + redirected URL has the marker → operational', async () => {
    global.fetch = jest
      .fn()
      .mockResolvedValue(fakeResponse(200, '<html>login</html>', 'https://crm.madfam.io/login'))

    const result = await checkService(
      makeService({ assertFinalUrlContains: 'crm.madfam.io/login' })
    )

    expect(result.status).toBe('operational')
    expect(result.statusCode).toBe(200)
    expect(result.error).toBeUndefined()
  })

  it('assertFinalUrlContains fail: 200 + redirected URL missing the marker → degraded', async () => {
    global.fetch = jest
      .fn()
      .mockResolvedValue(fakeResponse(200, '<html>landing</html>', 'https://crm.madfam.io/'))

    const result = await checkService(
      makeService({ assertFinalUrlContains: 'crm.madfam.io/login' })
    )

    expect(result.status).toBe('degraded')
    expect(result.statusCode).toBe(200)
    expect(result.error).toBe('final URL missing required content')
  })

  it('assertFinalUrlNotContains fail: 200 + redirected URL has the forbidden marker → degraded', async () => {
    global.fetch = jest
      .fn()
      .mockResolvedValue(fakeResponse(200, '<html>generic</html>', 'https://crm.phynd.app/landing'))

    const result = await checkService(
      makeService({ assertFinalUrlNotContains: '/landing' })
    )

    expect(result.status).toBe('degraded')
    expect(result.statusCode).toBe(200)
    expect(result.error).toBe('final URL contains forbidden content')
  })
})

describe('checkAllServices', () => {
  it('checks all services in parallel and returns results', async () => {
    global.fetch = jest.fn().mockResolvedValue(fakeResponse(200))

    const services = [
      makeService({ name: 'API', url: 'https://api.test.com' }),
      makeService({ name: 'Web', url: 'https://web.test.com' }),
      makeService({ name: 'Admin', url: 'https://admin.test.com' }),
    ]

    const results = await checkAllServices(services)
    expect(results).toHaveLength(3)
    expect(results[0].service).toBe('API')
    expect(results[1].service).toBe('Web')
    expect(results[2].service).toBe('Admin')
    expect(results.every((r) => r.status === 'operational')).toBe(true)
  })

  it('returns empty array for empty services', async () => {
    const results = await checkAllServices([])
    expect(results).toEqual([])
  })
})

describe('clearCache', () => {
  it('empties the cache', async () => {
    global.fetch = jest.fn().mockResolvedValue(fakeResponse(200))

    await checkService(makeService())
    expect(getCacheStats().size).toBe(1)

    clearCache()
    expect(getCacheStats().size).toBe(0)
    expect(getCacheStats().entries).toEqual([])
  })
})

describe('getCacheStats', () => {
  it('returns size and entries list', async () => {
    global.fetch = jest.fn().mockResolvedValue(fakeResponse(200))

    const service1 = makeService({ name: 'API', url: 'https://api.test.com' })
    const service2 = makeService({ name: 'Web', url: 'https://web.test.com' })

    await checkService(service1)
    await checkService(service2)

    const stats = getCacheStats()
    expect(stats.size).toBe(2)
    expect(stats.entries).toContain('https://api.test.com')
    expect(stats.entries).toContain('https://web.test.com')
  })
})
