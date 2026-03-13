import {
  STATUS_COLORS,
  STATUS_LABELS,
  STATUS_CONFIG,
  STATUS_PRIORITY,
  RESPONSE_TIME_COLORS,
  RESPONSE_TIME_LABELS,
  INCIDENT_STATUS_CONFIG,
  INCIDENT_SEVERITY_CONFIG,
  groupByKey,
} from '@/lib/status-config'
import type { ServiceStatus, IncidentStatus, IncidentSeverity } from '@/lib/types'

const ALL_SERVICE_STATUSES: ServiceStatus[] = [
  'operational',
  'degraded',
  'outage',
  'maintenance',
  'unknown',
]

const ALL_INCIDENT_STATUSES: IncidentStatus[] = [
  'investigating',
  'identified',
  'monitoring',
  'resolved',
]

const ALL_INCIDENT_SEVERITIES: IncidentSeverity[] = ['minor', 'major', 'critical']

describe('STATUS_COLORS', () => {
  it('has color classes for all 5 service statuses', () => {
    for (const status of ALL_SERVICE_STATUSES) {
      expect(STATUS_COLORS[status]).toBeDefined()
      expect(typeof STATUS_COLORS[status]).toBe('string')
      expect(STATUS_COLORS[status].length).toBeGreaterThan(0)
    }
  })
})

describe('STATUS_LABELS', () => {
  it('has correct human-readable labels for each status', () => {
    expect(STATUS_LABELS.operational).toBe('Operational')
    expect(STATUS_LABELS.degraded).toBe('Degraded')
    expect(STATUS_LABELS.outage).toBe('Major Outage')
    expect(STATUS_LABELS.maintenance).toBe('Maintenance')
    expect(STATUS_LABELS.unknown).toBe('Unknown')
  })
})

describe('STATUS_CONFIG', () => {
  it('has label, dotClass, bgClass, and textClass for each status', () => {
    for (const status of ALL_SERVICE_STATUSES) {
      const config = STATUS_CONFIG[status]
      expect(config).toBeDefined()
      expect(typeof config.label).toBe('string')
      expect(typeof config.dotClass).toBe('string')
      expect(typeof config.bgClass).toBe('string')
      expect(typeof config.textClass).toBe('string')
    }
  })

  it('labels match STATUS_LABELS', () => {
    for (const status of ALL_SERVICE_STATUSES) {
      expect(STATUS_CONFIG[status].label).toBe(STATUS_LABELS[status])
    }
  })
})

describe('STATUS_PRIORITY', () => {
  it('outage has lowest priority value (worst)', () => {
    expect(STATUS_PRIORITY.outage).toBe(0)
  })

  it('operational has highest priority value (best)', () => {
    expect(STATUS_PRIORITY.operational).toBe(4)
  })

  it('follows correct ordering: outage < degraded < maintenance < unknown < operational', () => {
    expect(STATUS_PRIORITY.outage).toBeLessThan(STATUS_PRIORITY.degraded)
    expect(STATUS_PRIORITY.degraded).toBeLessThan(STATUS_PRIORITY.maintenance)
    expect(STATUS_PRIORITY.maintenance).toBeLessThan(STATUS_PRIORITY.unknown)
    expect(STATUS_PRIORITY.unknown).toBeLessThan(STATUS_PRIORITY.operational)
  })
})

describe('RESPONSE_TIME_COLORS', () => {
  it('has color classes for all response time statuses', () => {
    const statuses = ['fast', 'normal', 'slow', 'critical', 'unknown'] as const
    for (const status of statuses) {
      expect(RESPONSE_TIME_COLORS[status]).toBeDefined()
      expect(typeof RESPONSE_TIME_COLORS[status]).toBe('string')
    }
  })
})

describe('RESPONSE_TIME_LABELS', () => {
  it('has human-readable labels for all response time statuses', () => {
    expect(RESPONSE_TIME_LABELS.fast).toBe('Fast')
    expect(RESPONSE_TIME_LABELS.normal).toBe('Normal')
    expect(RESPONSE_TIME_LABELS.slow).toBe('Slow')
    expect(RESPONSE_TIME_LABELS.critical).toBe('Very Slow')
    expect(RESPONSE_TIME_LABELS.unknown).toBe('Unknown')
  })
})

describe('INCIDENT_STATUS_CONFIG', () => {
  it('has config for all 4 incident statuses', () => {
    for (const status of ALL_INCIDENT_STATUSES) {
      const config = INCIDENT_STATUS_CONFIG[status]
      expect(config).toBeDefined()
      expect(typeof config.label).toBe('string')
      expect(typeof config.color).toBe('string')
      expect(typeof config.bgColor).toBe('string')
      expect(config.icon).toBeDefined()
    }
  })

  it('has correct labels', () => {
    expect(INCIDENT_STATUS_CONFIG.investigating.label).toBe('Investigating')
    expect(INCIDENT_STATUS_CONFIG.identified.label).toBe('Identified')
    expect(INCIDENT_STATUS_CONFIG.monitoring.label).toBe('Monitoring')
    expect(INCIDENT_STATUS_CONFIG.resolved.label).toBe('Resolved')
  })
})

describe('INCIDENT_SEVERITY_CONFIG', () => {
  it('has config for all 3 severity levels', () => {
    for (const severity of ALL_INCIDENT_SEVERITIES) {
      const config = INCIDENT_SEVERITY_CONFIG[severity]
      expect(config).toBeDefined()
      expect(typeof config.label).toBe('string')
      expect(typeof config.color).toBe('string')
      expect(config.icon).toBeDefined()
    }
  })

  it('has correct labels', () => {
    expect(INCIDENT_SEVERITY_CONFIG.minor.label).toBe('Minor')
    expect(INCIDENT_SEVERITY_CONFIG.major.label).toBe('Major')
    expect(INCIDENT_SEVERITY_CONFIG.critical.label).toBe('Critical')
  })
})

describe('groupByKey', () => {
  it('groups items correctly by a key function', () => {
    const items = [
      { name: 'a', category: 'x' },
      { name: 'b', category: 'y' },
      { name: 'c', category: 'x' },
      { name: 'd', category: 'y' },
      { name: 'e', category: 'z' },
    ]
    const groups = groupByKey(items, (item) => item.category)

    expect(groups.size).toBe(3)
    expect(groups.get('x')).toEqual([
      { name: 'a', category: 'x' },
      { name: 'c', category: 'x' },
    ])
    expect(groups.get('y')).toEqual([
      { name: 'b', category: 'y' },
      { name: 'd', category: 'y' },
    ])
    expect(groups.get('z')).toEqual([{ name: 'e', category: 'z' }])
  })

  it('returns empty map for empty array', () => {
    const groups = groupByKey([], (item: { key: string }) => item.key)
    expect(groups.size).toBe(0)
  })

  it('handles single item', () => {
    const groups = groupByKey([{ id: 1, type: 'foo' }], (item) => item.type)
    expect(groups.size).toBe(1)
    expect(groups.get('foo')).toEqual([{ id: 1, type: 'foo' }])
  })

  it('handles all items with the same key', () => {
    const items = [
      { val: 1, group: 'same' },
      { val: 2, group: 'same' },
      { val: 3, group: 'same' },
    ]
    const groups = groupByKey(items, (item) => item.group)
    expect(groups.size).toBe(1)
    expect(groups.get('same')!.length).toBe(3)
  })
})
