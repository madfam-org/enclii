import type { ServiceConfig, HealthCheckResult, ServiceStatus } from './types'
import { getHealthCheckTimeout, getCacheTTL, getRetryCount, getRetryDelayMs } from './config'

/**
 * Simple in-memory cache for health check results
 */
interface CacheEntry {
  result: HealthCheckResult
  expiresAt: number
}

const cache = new Map<string, CacheEntry>()

interface CheckUrlResult {
  status: ServiceStatus
  responseTime: number | null
  statusCode?: number
  error?: string
  retryable: boolean
}

/**
 * Options for content-match assertions on the probe response body.
 * Assertions only run when at least one is set; otherwise the body is never
 * read (zero overhead for the existing fast-path).
 */
interface AssertionOptions {
  assertContains?: string
  assertNotContains?: string
}

/** Hard cap on body bytes read for content-match assertions (1 MiB). */
const MAX_ASSERT_BODY_BYTES = 1024 * 1024

/**
 * Read up to MAX_ASSERT_BODY_BYTES of a Response body as UTF-8 text.
 * Reading via the byte stream lets us bail out on huge SPA bundles instead
 * of buffering the whole thing into memory.
 *
 * Falls back to `response.text()` (and a post-hoc truncation) when the
 * stream API is unavailable on the runtime — happens in jsdom-mocked
 * Response objects in unit tests.
 */
async function readBodyCapped(response: Response): Promise<string> {
  const body = response.body
  if (!body || typeof body.getReader !== 'function') {
    const full = await response.text()
    return full.length > MAX_ASSERT_BODY_BYTES
      ? full.slice(0, MAX_ASSERT_BODY_BYTES)
      : full
  }

  const reader = body.getReader()
  const decoder = new TextDecoder('utf-8', { fatal: false })
  const chunks: string[] = []
  let total = 0

  try {
    while (total < MAX_ASSERT_BODY_BYTES) {
      const { value, done } = await reader.read()
      if (done) break
      if (!value) continue
      const remaining = MAX_ASSERT_BODY_BYTES - total
      const slice = value.byteLength > remaining ? value.subarray(0, remaining) : value
      chunks.push(decoder.decode(slice, { stream: true }))
      total += slice.byteLength
      if (total >= MAX_ASSERT_BODY_BYTES) break
    }
    chunks.push(decoder.decode())
  } finally {
    // Free the upstream connection — important when we bail mid-stream.
    try { await reader.cancel() } catch { /* ignore */ }
  }

  return chunks.join('')
}

/**
 * Apply content-match assertions to a 2xx response. Returns a degraded result
 * when an assertion fails, or null when all assertions pass (or none are set).
 *
 * Catches body-read errors (e.g. malformed encoding, abort mid-stream) and
 * marks the service degraded — better than silently falling through to
 * "operational" while the assertion was never actually verified.
 */
async function evaluateAssertions(
  response: Response,
  responseTime: number,
  statusCode: number,
  opts: AssertionOptions
): Promise<CheckUrlResult | null> {
  if (!opts.assertContains && !opts.assertNotContains) return null

  let body: string
  try {
    body = await readBodyCapped(response)
  } catch (err) {
    const message = err instanceof Error ? err.message : 'unknown error'
    return {
      status: 'degraded',
      responseTime,
      statusCode,
      error: `failed to read body for assertion: ${message}`,
      retryable: false,
    }
  }

  if (opts.assertContains && !body.includes(opts.assertContains)) {
    return {
      status: 'degraded',
      responseTime,
      statusCode,
      error: 'body missing required content',
      retryable: false,
    }
  }

  if (opts.assertNotContains && body.includes(opts.assertNotContains)) {
    return {
      status: 'degraded',
      responseTime,
      statusCode,
      error: 'body contains forbidden content',
      retryable: false,
    }
  }

  return null
}

/**
 * Check if a URL is healthy
 * Returns status, response time, and whether the result is retryable.
 * Retryable means a transient issue that may resolve on retry (5xx, timeouts, network errors).
 * Non-retryable: 2xx (success), 4xx (client error), 503 (maintenance).
 *
 * When `assertions` carries an `assertContains` and/or `assertNotContains`,
 * the probe additionally reads the response body (capped at 1 MiB) and
 * downgrades a 2xx to `degraded` when content rules fail. Bodies are not
 * read when no assertion is configured (zero overhead for legacy services).
 */
async function checkUrl(
  url: string,
  timeout: number,
  assertions: AssertionOptions = {}
): Promise<CheckUrlResult> {
  const startTime = Date.now()

  try {
    const controller = new AbortController()
    const timeoutId = setTimeout(() => controller.abort(), timeout)

    const response = await fetch(url, {
      method: 'GET',
      signal: controller.signal,
      headers: {
        'User-Agent': 'Enclii-Status-Monitor/1.0',
        'Accept': 'text/html,application/json',
      },
      redirect: 'follow',
    })

    clearTimeout(timeoutId)

    const responseTime = Date.now() - startTime
    const statusCode = response.status

    // 2xx = operational — but optionally subject to content-match assertions.
    if (statusCode >= 200 && statusCode < 300) {
      const assertionFailure = await evaluateAssertions(
        response,
        responseTime,
        statusCode,
        assertions
      )
      if (assertionFailure) return assertionFailure
      return { status: 'operational', responseTime, statusCode, retryable: false }
    }

    // 503 often indicates maintenance — not retryable (intentional)
    if (statusCode === 503) {
      return { status: 'maintenance', responseTime, statusCode, retryable: false }
    }

    // 4xx = degraded — not retryable (client-side issue)
    if (statusCode >= 400 && statusCode < 500) {
      return { status: 'degraded', responseTime, statusCode, error: `HTTP ${statusCode}`, retryable: false }
    }

    // 5xx (non-503) = outage — retryable (likely transient)
    if (statusCode >= 500) {
      return { status: 'outage', responseTime, statusCode, error: `HTTP ${statusCode}`, retryable: true }
    }

    // Other status codes - treat as degraded, not retryable
    return { status: 'degraded', responseTime, statusCode, retryable: false }

  } catch (err) {
    const responseTime = Date.now() - startTime

    if (err instanceof Error) {
      // Timeout — retryable
      if (err.name === 'AbortError') {
        return { status: 'outage', responseTime: timeout, error: 'Request timed out', retryable: true }
      }

      // Network errors — retryable
      if (err.message.includes('ECONNREFUSED')) {
        return { status: 'outage', responseTime, error: 'Connection refused', retryable: true }
      }

      if (err.message.includes('ENOTFOUND')) {
        return { status: 'outage', responseTime, error: 'DNS lookup failed', retryable: true }
      }

      // SSL/TLS errors — retryable (could be transient cert propagation)
      if (err.message.includes('certificate') || err.message.includes('SSL')) {
        return { status: 'outage', responseTime, error: 'SSL/TLS error', retryable: true }
      }

      return { status: 'outage', responseTime, error: err.message, retryable: true }
    }

    return { status: 'outage', responseTime, error: 'Unknown error', retryable: true }
  }
}

/**
 * Sleep for a given number of milliseconds
 */
function sleep(ms: number): Promise<void> {
  return new Promise(resolve => setTimeout(resolve, ms))
}

/**
 * Check health of a single service
 */
export async function checkService(service: ServiceConfig): Promise<HealthCheckResult> {
  // probeUrl overrides url for the actual probe (so url can stay a
  // human-friendly link); falls back to url for backwards compatibility.
  const probeTarget = service.probeUrl ?? service.url
  const cacheKey = probeTarget
  const now = Date.now()
  const ttl = getCacheTTL() * 1000

  // Check cache
  const cached = cache.get(cacheKey)
  if (cached && cached.expiresAt > now) {
    return cached.result
  }

  // Perform health check with retry logic for transient failures
  const timeout = getHealthCheckTimeout()
  const maxRetries = getRetryCount()
  const baseDelay = getRetryDelayMs()

  const assertions: AssertionOptions = {
    ...(service.assertContains !== undefined && { assertContains: service.assertContains }),
    ...(service.assertNotContains !== undefined && { assertNotContains: service.assertNotContains }),
  }

  let checkResult = await checkUrl(probeTarget, timeout, assertions)

  // Retry if non-operational AND retryable (transient errors like 502, timeouts)
  if (checkResult.status !== 'operational' && checkResult.retryable && maxRetries > 0) {
    for (let attempt = 0; attempt < maxRetries; attempt++) {
      await sleep(baseDelay * Math.pow(2, attempt))
      checkResult = await checkUrl(probeTarget, timeout, assertions)
      // Stop retrying if we get a definitive result
      if (checkResult.status === 'operational' || !checkResult.retryable) break
    }
  }

  const { status, responseTime, statusCode, error } = checkResult

  const result: HealthCheckResult = {
    service: service.name,
    url: service.url,
    ...(service.href && { href: service.href }),
    group: service.group,
    ...(service.family && { family: service.family }),
    description: service.description,
    status,
    responseTime,
    lastChecked: new Date().toISOString(),
    statusCode,
    error,
  }

  // Update cache
  cache.set(cacheKey, {
    result,
    expiresAt: now + ttl,
  })

  return result
}

/**
 * Check health of all services
 */
export async function checkAllServices(services: ServiceConfig[]): Promise<HealthCheckResult[]> {
  // Run all health checks in parallel
  const results = await Promise.all(services.map(service => checkService(service)))
  return results
}

/**
 * Clear the health check cache
 */
export function clearCache(): void {
  cache.clear()
}

/**
 * Get cache statistics
 */
export function getCacheStats(): { size: number; entries: string[] } {
  return {
    size: cache.size,
    entries: Array.from(cache.keys()),
  }
}
