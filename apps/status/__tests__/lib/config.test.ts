import {
  getSiteConfig,
  getHealthCheckTimeout,
  getCacheTTL,
  getPrometheusUrl,
  getDatabaseUrl,
  getRetryCount,
  getRetryDelayMs,
  getAutoIncidentConfig,
  getResponseTimeThresholds,
} from '@/lib/config'

// Save and restore environment variables around each test
const envBackup: Record<string, string | undefined> = {}
const envKeys = [
  'SITE_NAME',
  'SITE_URL',
  'NEXT_PUBLIC_APP_URL',
  'SERVICES_CONFIG',
  'HEALTH_CHECK_TIMEOUT_MS',
  'HEALTH_CHECK_CACHE_TTL_SECONDS',
  'PROMETHEUS_URL',
  'DATABASE_URL',
  'HEALTH_CHECK_RETRIES',
  'HEALTH_CHECK_RETRY_DELAY_MS',
  'AUTO_INCIDENTS_ENABLED',
  'AUTO_INCIDENT_THRESHOLD',
  'RESPONSE_TIME_THRESHOLDS',
]

beforeEach(() => {
  for (const key of envKeys) {
    envBackup[key] = process.env[key]
    delete process.env[key]
  }
})

afterEach(() => {
  for (const key of envKeys) {
    if (envBackup[key] !== undefined) {
      process.env[key] = envBackup[key]
    } else {
      delete process.env[key]
    }
  }
})

describe('getSiteConfig', () => {
  it('returns defaults when no env vars are set', () => {
    const config = getSiteConfig()
    expect(config.name).toBe('Enclii Status')
    expect(config.url).toBe('https://status.enclii.dev')
    expect(config.services).toBeDefined()
    expect(config.services.length).toBe(4) // default Enclii services
  })

  it('uses SITE_NAME and SITE_URL from env', () => {
    process.env.SITE_NAME = 'Custom Status'
    process.env.SITE_URL = 'https://custom.example.com'

    const config = getSiteConfig()
    expect(config.name).toBe('Custom Status')
    expect(config.url).toBe('https://custom.example.com')
  })

  it('falls back to NEXT_PUBLIC_APP_URL when SITE_URL is not set', () => {
    process.env.NEXT_PUBLIC_APP_URL = 'https://next-public.example.com'

    const config = getSiteConfig()
    expect(config.url).toBe('https://next-public.example.com')
  })

  it('parses valid SERVICES_CONFIG JSON array', () => {
    process.env.SERVICES_CONFIG = JSON.stringify([
      { name: 'API', url: 'https://api.test.com/health', group: 'Core' },
      { name: 'Web', url: 'https://web.test.com', group: 'Core' },
    ])

    const config = getSiteConfig()
    expect(config.services.length).toBe(2)
    expect(config.services[0].name).toBe('API')
    expect(config.services[1].group).toBe('Core')
  })

  it('preserves optional `family` field on services (RFC 0002 S1)', () => {
    process.env.SERVICES_CONFIG = JSON.stringify([
      {
        name: 'Karafiel API',
        url: 'https://api.karafiel.mx/health',
        group: 'Karafiel',
        family: 'MADFAM Platform',
      },
      {
        // No family — legacy entry, should still parse.
        name: 'Legacy Thing',
        url: 'https://legacy.example.com',
        group: 'Legacy',
      },
    ])

    const config = getSiteConfig()
    expect(config.services).toHaveLength(2)
    expect(config.services[0].family).toBe('MADFAM Platform')
    expect(config.services[1].family).toBeUndefined()
  })

  it('falls back to defaults on invalid JSON', () => {
    process.env.SERVICES_CONFIG = 'not valid json {'

    const consoleSpy = jest.spyOn(console, 'error').mockImplementation()
    const config = getSiteConfig()
    expect(config.services.length).toBe(4) // defaults
    consoleSpy.mockRestore()
  })

  it('falls back to defaults when SERVICES_CONFIG is not an array', () => {
    process.env.SERVICES_CONFIG = JSON.stringify({ name: 'not an array' })

    const consoleSpy = jest.spyOn(console, 'error').mockImplementation()
    const config = getSiteConfig()
    expect(config.services.length).toBe(4) // defaults
    consoleSpy.mockRestore()
  })

  it('skips services missing required fields (name, url, group)', () => {
    process.env.SERVICES_CONFIG = JSON.stringify([
      { name: 'Valid', url: 'https://valid.com', group: 'Core' },
      { name: 'Missing URL', group: 'Core' },            // no url
      { url: 'https://noname.com', group: 'Core' },       // no name
      { name: 'No Group', url: 'https://nogroup.com' },   // no group
    ])

    const consoleSpy = jest.spyOn(console, 'warn').mockImplementation()
    const config = getSiteConfig()
    expect(config.services.length).toBe(1)
    expect(config.services[0].name).toBe('Valid')
    consoleSpy.mockRestore()
  })
})

describe('getHealthCheckTimeout', () => {
  it('returns default 10000 when env not set', () => {
    expect(getHealthCheckTimeout()).toBe(10000)
  })

  it('reads HEALTH_CHECK_TIMEOUT_MS from env', () => {
    process.env.HEALTH_CHECK_TIMEOUT_MS = '5000'
    expect(getHealthCheckTimeout()).toBe(5000)
  })
})

describe('getCacheTTL', () => {
  it('returns default 30 when env not set', () => {
    expect(getCacheTTL()).toBe(30)
  })

  it('reads HEALTH_CHECK_CACHE_TTL_SECONDS from env', () => {
    process.env.HEALTH_CHECK_CACHE_TTL_SECONDS = '60'
    expect(getCacheTTL()).toBe(60)
  })
})

describe('getPrometheusUrl', () => {
  it('returns null when PROMETHEUS_URL is not set', () => {
    expect(getPrometheusUrl()).toBeNull()
  })

  it('returns the URL when set', () => {
    process.env.PROMETHEUS_URL = 'http://prometheus:9090'
    expect(getPrometheusUrl()).toBe('http://prometheus:9090')
  })
})

describe('getDatabaseUrl', () => {
  it('returns null when DATABASE_URL is not set', () => {
    expect(getDatabaseUrl()).toBeNull()
  })

  it('returns the URL when set', () => {
    process.env.DATABASE_URL = 'postgresql://user:pass@localhost:5432/status'
    expect(getDatabaseUrl()).toBe('postgresql://user:pass@localhost:5432/status')
  })
})

describe('getRetryCount', () => {
  it('returns default 2 when env not set', () => {
    expect(getRetryCount()).toBe(2)
  })

  it('reads HEALTH_CHECK_RETRIES from env', () => {
    process.env.HEALTH_CHECK_RETRIES = '5'
    expect(getRetryCount()).toBe(5)
  })
})

describe('getRetryDelayMs', () => {
  it('returns default 1000 when env not set', () => {
    expect(getRetryDelayMs()).toBe(1000)
  })

  it('reads HEALTH_CHECK_RETRY_DELAY_MS from env', () => {
    process.env.HEALTH_CHECK_RETRY_DELAY_MS = '500'
    expect(getRetryDelayMs()).toBe(500)
  })
})

describe('getAutoIncidentConfig', () => {
  it('defaults to enabled=true and threshold=2', () => {
    const config = getAutoIncidentConfig()
    expect(config.enabled).toBe(true)
    expect(config.threshold).toBe(2)
  })

  it('reads AUTO_INCIDENTS_ENABLED=false', () => {
    process.env.AUTO_INCIDENTS_ENABLED = 'false'
    const config = getAutoIncidentConfig()
    expect(config.enabled).toBe(false)
  })

  it('treats any non-false value as enabled', () => {
    process.env.AUTO_INCIDENTS_ENABLED = 'true'
    expect(getAutoIncidentConfig().enabled).toBe(true)

    process.env.AUTO_INCIDENTS_ENABLED = 'yes'
    expect(getAutoIncidentConfig().enabled).toBe(true)
  })

  it('reads AUTO_INCIDENT_THRESHOLD from env', () => {
    process.env.AUTO_INCIDENT_THRESHOLD = '5'
    expect(getAutoIncidentConfig().threshold).toBe(5)
  })
})

describe('getResponseTimeThresholds', () => {
  it('returns defaults when env not set', () => {
    const thresholds = getResponseTimeThresholds()
    expect(thresholds).toEqual({ fast: 200, normal: 500, slow: 1000 })
  })

  it('parses custom JSON thresholds from env', () => {
    process.env.RESPONSE_TIME_THRESHOLDS = JSON.stringify({
      fast: 1500,
      normal: 2500,
      slow: 4000,
    })

    const thresholds = getResponseTimeThresholds()
    expect(thresholds).toEqual({ fast: 1500, normal: 2500, slow: 4000 })
  })

  it('falls back to defaults on invalid JSON', () => {
    process.env.RESPONSE_TIME_THRESHOLDS = 'not json'
    const thresholds = getResponseTimeThresholds()
    expect(thresholds).toEqual({ fast: 200, normal: 500, slow: 1000 })
  })
})
