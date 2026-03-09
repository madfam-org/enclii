import type { ServiceConfig, HealthCheckResult, ServiceStatus } from './types'
import { getHealthCheckTimeout, getCacheTTL } from './config'

/**
 * Simple in-memory cache for health check results
 */
interface CacheEntry {
  result: HealthCheckResult
  expiresAt: number
}

const cache = new Map<string, CacheEntry>()

/**
 * Check if a URL is healthy
 * Returns status and response time
 */
async function checkUrl(url: string, timeout: number): Promise<{
  status: ServiceStatus
  responseTime: number | null
  statusCode?: number
  error?: string
}> {
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
      // Don't follow too many redirects
      redirect: 'follow',
    })

    clearTimeout(timeoutId)

    const responseTime = Date.now() - startTime
    const statusCode = response.status

    // 2xx = operational
    if (statusCode >= 200 && statusCode < 300) {
      return { status: 'operational', responseTime, statusCode }
    }

    // 503 often indicates maintenance
    if (statusCode === 503) {
      return { status: 'maintenance', responseTime, statusCode }
    }

    // 4xx/5xx = degraded or outage
    if (statusCode >= 400 && statusCode < 500) {
      return { status: 'degraded', responseTime, statusCode, error: `HTTP ${statusCode}` }
    }

    if (statusCode >= 500) {
      return { status: 'outage', responseTime, statusCode, error: `HTTP ${statusCode}` }
    }

    // Other status codes - treat as degraded
    return { status: 'degraded', responseTime, statusCode }

  } catch (err) {
    const responseTime = Date.now() - startTime

    if (err instanceof Error) {
      // Timeout
      if (err.name === 'AbortError') {
        return {
          status: 'outage',
          responseTime: timeout,
          error: 'Request timed out'
        }
      }

      // Network errors
      if (err.message.includes('ECONNREFUSED')) {
        return { status: 'outage', responseTime, error: 'Connection refused' }
      }

      if (err.message.includes('ENOTFOUND')) {
        return { status: 'outage', responseTime, error: 'DNS lookup failed' }
      }

      // SSL/TLS errors
      if (err.message.includes('certificate') || err.message.includes('SSL')) {
        return { status: 'outage', responseTime, error: 'SSL/TLS error' }
      }

      return { status: 'outage', responseTime, error: err.message }
    }

    return { status: 'outage', responseTime, error: 'Unknown error' }
  }
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

  // Perform health check
  const timeout = getHealthCheckTimeout()
  const { status, responseTime, statusCode, error } = await checkUrl(service.url, timeout)

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
