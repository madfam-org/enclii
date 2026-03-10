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
 * Check if a URL is healthy
 * Returns status, response time, and whether the result is retryable.
 * Retryable means a transient issue that may resolve on retry (5xx, timeouts, network errors).
 * Non-retryable: 2xx (success), 4xx (client error), 503 (maintenance).
 */
async function checkUrl(url: string, timeout: number): Promise<CheckUrlResult> {
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

    // 2xx = operational — not retryable
    if (statusCode >= 200 && statusCode < 300) {
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
  const cacheKey = service.url
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

  let checkResult = await checkUrl(service.url, timeout)

  // Retry if non-operational AND retryable (transient errors like 502, timeouts)
  if (checkResult.status !== 'operational' && checkResult.retryable && maxRetries > 0) {
    for (let attempt = 0; attempt < maxRetries; attempt++) {
      await sleep(baseDelay * Math.pow(2, attempt))
      checkResult = await checkUrl(service.url, timeout)
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
