import {
  getResponseTimeStatus,
  calculateOverallStatus,
  RESPONSE_TIME_THRESHOLDS,
} from '@/lib/types'
import type { HealthCheckResult, ResponseTimeThresholds } from '@/lib/types'

// Helper to build a minimal HealthCheckResult for testing
function makeResult(status: HealthCheckResult['status']): HealthCheckResult {
  return {
    service: 'test-service',
    url: 'https://example.com/health',
    group: 'Test',
    status,
    responseTime: 100,
    lastChecked: new Date().toISOString(),
  }
}

describe('getResponseTimeStatus', () => {
  it('returns unknown for null', () => {
    expect(getResponseTimeStatus(null)).toBe('unknown')
  })

  it('returns fast for response time below fast threshold', () => {
    expect(getResponseTimeStatus(50)).toBe('fast')
    expect(getResponseTimeStatus(0)).toBe('fast')
    expect(getResponseTimeStatus(199)).toBe('fast')
  })

  it('returns normal for response time between fast and normal thresholds', () => {
    expect(getResponseTimeStatus(200)).toBe('normal')
    expect(getResponseTimeStatus(350)).toBe('normal')
    expect(getResponseTimeStatus(499)).toBe('normal')
  })

  it('returns slow for response time between normal and slow thresholds', () => {
    expect(getResponseTimeStatus(500)).toBe('slow')
    expect(getResponseTimeStatus(750)).toBe('slow')
    expect(getResponseTimeStatus(999)).toBe('slow')
  })

  it('returns critical for response time above slow threshold', () => {
    expect(getResponseTimeStatus(1000)).toBe('critical')
    expect(getResponseTimeStatus(5000)).toBe('critical')
    expect(getResponseTimeStatus(99999)).toBe('critical')
  })

  it('uses default thresholds from RESPONSE_TIME_THRESHOLDS', () => {
    expect(RESPONSE_TIME_THRESHOLDS.fast).toBe(200)
    expect(RESPONSE_TIME_THRESHOLDS.normal).toBe(500)
    expect(RESPONSE_TIME_THRESHOLDS.slow).toBe(1000)
  })

  it('respects custom thresholds', () => {
    const custom: ResponseTimeThresholds = { fast: 100, normal: 300, slow: 600 }
    expect(getResponseTimeStatus(50, custom)).toBe('fast')
    expect(getResponseTimeStatus(100, custom)).toBe('normal')
    expect(getResponseTimeStatus(300, custom)).toBe('slow')
    expect(getResponseTimeStatus(600, custom)).toBe('critical')
  })

  it('handles exact boundary values with default thresholds', () => {
    // Boundaries are exclusive: < fast, < normal, < slow
    expect(getResponseTimeStatus(200)).toBe('normal') // not fast (>= 200)
    expect(getResponseTimeStatus(500)).toBe('slow')   // not normal (>= 500)
    expect(getResponseTimeStatus(1000)).toBe('critical') // not slow (>= 1000)
  })
})

describe('calculateOverallStatus', () => {
  it('returns unknown for empty array', () => {
    expect(calculateOverallStatus([])).toBe('unknown')
  })

  it('returns operational when all services are operational', () => {
    const services = [
      makeResult('operational'),
      makeResult('operational'),
      makeResult('operational'),
    ]
    expect(calculateOverallStatus(services)).toBe('operational')
  })

  it('returns degraded when degraded is worst status present', () => {
    const services = [
      makeResult('operational'),
      makeResult('degraded'),
      makeResult('operational'),
    ]
    expect(calculateOverallStatus(services)).toBe('degraded')
  })

  it('returns outage when outage is present (worst possible)', () => {
    const services = [
      makeResult('operational'),
      makeResult('degraded'),
      makeResult('outage'),
    ]
    expect(calculateOverallStatus(services)).toBe('outage')
  })

  it('returns outage even with a single outage service', () => {
    const services = [makeResult('outage')]
    expect(calculateOverallStatus(services)).toBe('outage')
  })

  it('returns maintenance when maintenance is worst status', () => {
    const services = [
      makeResult('operational'),
      makeResult('maintenance'),
    ]
    expect(calculateOverallStatus(services)).toBe('maintenance')
  })

  it('returns unknown when unknown is worst status', () => {
    const services = [
      makeResult('operational'),
      makeResult('unknown'),
    ]
    expect(calculateOverallStatus(services)).toBe('unknown')
  })

  it('returns worst status across all mixed statuses', () => {
    // outage (0) < degraded (1) < maintenance (2) < unknown (3) < operational (4)
    const services = [
      makeResult('operational'),
      makeResult('degraded'),
      makeResult('maintenance'),
      makeResult('unknown'),
    ]
    expect(calculateOverallStatus(services)).toBe('degraded')
  })
})
