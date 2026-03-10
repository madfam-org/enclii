import type { ServiceConfig, SiteConfig, ResponseTimeThresholds } from './types'

/**
 * Default services for Enclii status page
 */
const DEFAULT_ENCLII_SERVICES: ServiceConfig[] = [
  {
    name: 'Switchyard API',
    url: 'https://api.enclii.dev/health/ready',
    href: 'https://api.enclii.dev',
    group: 'Enclii',
    description: 'Control plane API',
  },
  {
    name: 'Web Dashboard',
    url: 'https://app.enclii.dev',
    group: 'Enclii',
    description: 'User dashboard',
  },
  {
    name: 'Admin Console',
    url: 'https://admin.enclii.dev',
    group: 'Enclii',
    description: 'Infrastructure admin',
  },
  {
    name: 'Documentation',
    url: 'https://docs.enclii.dev',
    group: 'Enclii',
    description: 'Documentation site',
  },
]

/**
 * Parse services configuration from environment
 */
function parseServicesConfig(): ServiceConfig[] {
  const configJson = process.env.SERVICES_CONFIG

  if (!configJson) {
    console.warn('SERVICES_CONFIG not set, using defaults')
    return DEFAULT_ENCLII_SERVICES
  }

  try {
    const parsed = JSON.parse(configJson)
    if (!Array.isArray(parsed)) {
      console.error('SERVICES_CONFIG must be a JSON array')
      return DEFAULT_ENCLII_SERVICES
    }

    // Validate each service config
    return parsed.filter((service): service is ServiceConfig => {
      if (!service.name || !service.url || !service.group) {
        console.warn('Skipping invalid service config:', service)
        return false
      }
      return true
    })
  } catch (err) {
    console.error('Failed to parse SERVICES_CONFIG:', err)
    return DEFAULT_ENCLII_SERVICES
  }
}

/**
 * Get site configuration from environment
 */
export function getSiteConfig(): SiteConfig {
  return {
    name: process.env.SITE_NAME || 'Enclii Status',
    url: process.env.SITE_URL || process.env.NEXT_PUBLIC_APP_URL || 'https://status.enclii.dev',
    services: parseServicesConfig(),
  }
}

/**
 * Get health check timeout from environment (default: 10 seconds)
 */
export function getHealthCheckTimeout(): number {
  const timeout = process.env.HEALTH_CHECK_TIMEOUT_MS
  return timeout ? parseInt(timeout, 10) : 10000
}

/**
 * Get health check cache TTL from environment (default: 30 seconds)
 */
export function getCacheTTL(): number {
  const ttl = process.env.HEALTH_CHECK_CACHE_TTL_SECONDS
  return ttl ? parseInt(ttl, 10) : 30
}

/**
 * Get Prometheus URL for historical data
 */
export function getPrometheusUrl(): string | null {
  return process.env.PROMETHEUS_URL || null
}

/**
 * Get database URL for incidents
 */
export function getDatabaseUrl(): string | null {
  return process.env.DATABASE_URL || null
}

/**
 * Get health check retry count (default: 2)
 * Retries reduce false outages from transient Cloudflare tunnel blips.
 */
export function getRetryCount(): number {
  const retries = process.env.HEALTH_CHECK_RETRIES
  return retries ? parseInt(retries, 10) : 2
}

/**
 * Get health check retry delay in ms (default: 1000)
 * Exponential backoff: baseDelay * 2^attempt
 */
export function getRetryDelayMs(): number {
  const delay = process.env.HEALTH_CHECK_RETRY_DELAY_MS
  return delay ? parseInt(delay, 10) : 1000
}

/**
 * Get response time thresholds from environment.
 * Allows per-deployment calibration (e.g. higher for Cloudflare tunnel latency).
 * Expects JSON: {"fast":1500,"normal":2500,"slow":4000}
 */
/**
 * Get auto-incident detection config from environment
 */
export function getAutoIncidentConfig(): { enabled: boolean; threshold: number } {
  return {
    enabled: process.env.AUTO_INCIDENTS_ENABLED !== 'false',
    threshold: parseInt(process.env.AUTO_INCIDENT_THRESHOLD ?? '2', 10),
  }
}

export function getResponseTimeThresholds(): ResponseTimeThresholds {
  const json = process.env.RESPONSE_TIME_THRESHOLDS
  if (json) {
    try { return JSON.parse(json) } catch { /* fall through */ }
  }
  return { fast: 200, normal: 500, slow: 1000 }
}

