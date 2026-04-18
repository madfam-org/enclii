import {
  buildSummary,
  stableId,
  mapServiceStatusToComponent,
  mapWorstStatusToIndicator,
  mapIncidentSeverityToImpact,
  mapIncidentStatus,
  type StatuspageComponent,
  type StatuspageComponentGroup,
} from '@/lib/statuspage-v2'
import type { HealthCheckResult, Incident, ScheduledMaintenance } from '@/lib/types'

// ---- Fixtures ----

const FIXED_NOW = new Date('2026-04-17T12:00:00.000Z')

function mkService(
  name: string,
  status: HealthCheckResult['status'],
  group: string,
  family?: string,
): HealthCheckResult {
  return {
    service: name,
    url: `https://${name.toLowerCase().replace(/\s+/g, '-')}.example.com/health`,
    group,
    ...(family && { family }),
    status,
    responseTime: 120,
    lastChecked: '2026-04-17T11:59:50.000Z',
    statusCode: 200,
  }
}

const SERVICES: HealthCheckResult[] = [
  mkService('Karafiel API', 'operational', 'Karafiel', 'MADFAM Platform'),
  mkService('Karafiel Web', 'operational', 'Karafiel', 'MADFAM Platform'),
  mkService('Dhanam API', 'degraded', 'Dhanam', 'MADFAM Platform'),
  mkService('Selva API', 'operational', 'Selva Office', 'Selva Swarm'),
  // A service with no family — should fall back to its `group` as the group name.
  mkService('Solo Legacy', 'operational', 'Legacy'),
]

const INCIDENT: Incident = {
  id: 'inc-001',
  title: 'Dhanam API latency spike',
  status: 'investigating',
  severity: 'major',
  affectedServices: ['Dhanam API'],
  createdAt: '2026-04-17T11:30:00.000Z',
  updates: [
    {
      id: 'upd-001',
      incidentId: 'inc-001',
      message: 'Investigating elevated p95 latency on Dhanam API.',
      status: 'investigating',
      createdAt: '2026-04-17T11:30:05.000Z',
    },
  ],
}

const MAINTENANCE: ScheduledMaintenance = {
  id: 'maint-001',
  title: 'Database rolling upgrade',
  affectedServices: ['Dhanam API', 'Karafiel API'],
  scheduledStart: '2026-04-17T13:00:00.000Z',
  scheduledEnd: '2026-04-17T14:00:00.000Z',
  createdAt: '2026-04-10T00:00:00.000Z',
}

function buildFixture() {
  return buildSummary({
    pageName: 'MADFAM',
    pageUrl: 'https://status.madfam.io',
    services: SERVICES,
    incidents: [INCIDENT],
    scheduledMaintenances: [MAINTENANCE],
    now: FIXED_NOW,
  })
}

// ---- Enum mapping tests ----

describe('statuspage-v2 enum mappings', () => {
  it('maps ServiceStatus to Statuspage component status with exact strings', () => {
    expect(mapServiceStatusToComponent('operational')).toBe('operational')
    expect(mapServiceStatusToComponent('degraded')).toBe('degraded_performance')
    expect(mapServiceStatusToComponent('maintenance')).toBe('under_maintenance')
    expect(mapServiceStatusToComponent('outage')).toBe('major_outage')
    // Unknown falls back to 'operational' (optimistic default)
    expect(mapServiceStatusToComponent('unknown')).toBe('operational')
  })

  it('maps worst status to Statuspage page indicator', () => {
    expect(mapWorstStatusToIndicator('operational').indicator).toBe('none')
    expect(mapWorstStatusToIndicator('degraded').indicator).toBe('minor')
    expect(mapWorstStatusToIndicator('outage').indicator).toBe('critical')
    expect(mapWorstStatusToIndicator('maintenance').indicator).toBe('maintenance')
    expect(mapWorstStatusToIndicator('unknown').indicator).toBe('none')
  })

  it('maps incident severity to impact', () => {
    expect(mapIncidentSeverityToImpact('minor')).toBe('minor')
    expect(mapIncidentSeverityToImpact('major')).toBe('major')
    expect(mapIncidentSeverityToImpact('critical')).toBe('critical')
  })

  it('preserves incident status literal strings (identity map)', () => {
    expect(mapIncidentStatus('investigating')).toBe('investigating')
    expect(mapIncidentStatus('identified')).toBe('identified')
    expect(mapIncidentStatus('monitoring')).toBe('monitoring')
    expect(mapIncidentStatus('resolved')).toBe('resolved')
  })
})

// ---- stableId ----

describe('stableId', () => {
  it('is deterministic', () => {
    expect(stableId('hello')).toBe(stableId('hello'))
  })
  it('varies by input', () => {
    expect(stableId('hello')).not.toBe(stableId('world'))
  })
  it('produces a 12-char hex string', () => {
    expect(stableId('foo')).toMatch(/^[a-f0-9]{12}$/)
  })
})

// ---- Full summary shape ----

describe('buildSummary shape', () => {
  const summary = buildFixture()

  it('has the top-level keys Statuspage clients expect', () => {
    expect(Object.keys(summary).sort()).toEqual([
      'components',
      'incidents',
      'page',
      'scheduled_maintenances',
      'status',
    ])
  })

  it('has a page with required fields', () => {
    expect(summary.page).toMatchObject({
      name: 'MADFAM',
      url: 'https://status.madfam.io',
      time_zone: 'Etc/UTC',
    })
    expect(summary.page.id).toMatch(/^[a-f0-9]{12}$/)
    expect(summary.page.updated_at).toBe(FIXED_NOW.toISOString())
  })

  it('surfaces overall status as "minor" when a service is degraded', () => {
    expect(summary.status.indicator).toBe('minor')
    expect(summary.status.description).toBe('Partially Degraded Service')
  })
})

// ---- Component & group integrity ----

describe('buildSummary components + groups', () => {
  const summary = buildFixture()
  const groups = summary.components.filter(
    (c): c is StatuspageComponentGroup => c.group === true,
  )
  const leaves = summary.components.filter(
    (c): c is StatuspageComponent => c.group === false,
  )

  it('emits one leaf component per service', () => {
    expect(leaves).toHaveLength(SERVICES.length)
  })

  it('prefers family over group when both are present', () => {
    const groupNames = new Set(groups.map(g => g.name))
    expect(groupNames.has('MADFAM Platform')).toBe(true)
    expect(groupNames.has('Selva Swarm')).toBe(true)
    // `Karafiel` and `Dhanam` are subgroups of "MADFAM Platform" and must not
    // appear as top-level groups when family is set.
    expect(groupNames.has('Karafiel')).toBe(false)
    expect(groupNames.has('Dhanam')).toBe(false)
  })

  it('falls back to `group` when family is absent', () => {
    const groupNames = new Set(groups.map(g => g.name))
    expect(groupNames.has('Legacy')).toBe(true)
  })

  it('keeps group_id FK from leaf -> group consistent', () => {
    const groupIds = new Set(groups.map(g => g.id))
    for (const leaf of leaves) {
      expect(leaf.group_id).not.toBeNull()
      expect(groupIds.has(leaf.group_id!)).toBe(true)
    }
  })

  it('keeps the group.components array consistent with leaves pointing to it', () => {
    for (const group of groups) {
      const pointedTo = leaves.filter(l => l.group_id === group.id).map(l => l.id)
      expect([...group.components].sort()).toEqual(pointedTo.sort())
    }
  })

  it('rolls group status up to the worst member', () => {
    const platform = groups.find(g => g.name === 'MADFAM Platform')!
    // Degraded Dhanam API lives under MADFAM Platform -> group should be degraded_performance
    expect(platform.status).toBe('degraded_performance')

    const swarm = groups.find(g => g.name === 'Selva Swarm')!
    expect(swarm.status).toBe('operational')
  })

  it('emits valid Statuspage component status enum values', () => {
    const allowed = new Set([
      'operational',
      'degraded_performance',
      'partial_outage',
      'major_outage',
      'under_maintenance',
    ])
    for (const c of summary.components) {
      expect(allowed.has(c.status)).toBe(true)
    }
  })

  it('assigns stable deterministic ids', () => {
    const again = buildFixture()
    expect(again.components.map(c => c.id)).toEqual(summary.components.map(c => c.id))
  })
})

// ---- Incident shape ----

describe('buildSummary incidents', () => {
  const summary = buildFixture()
  const incident = summary.incidents[0]

  it('carries through title, impact, status', () => {
    expect(incident.name).toBe('Dhanam API latency spike')
    expect(incident.impact).toBe('major')
    expect(incident.status).toBe('investigating')
  })

  it('links components by id FK', () => {
    expect(incident.components).toHaveLength(1)
    const dhanamApi = summary.components.find(c => c.name === 'Dhanam API')!
    expect(incident.components[0].id).toBe(dhanamApi.id)
  })

  it('keeps incident_updates with Statuspage field names', () => {
    const upd = incident.incident_updates[0]
    expect(upd.body).toBe('Investigating elevated p95 latency on Dhanam API.')
    expect(upd.status).toBe('investigating')
    expect(upd.incident_id).toBe(incident.id)
    expect(upd.created_at).toBe('2026-04-17T11:30:05.000Z')
  })

  it('builds a shortlink under the page URL', () => {
    expect(incident.shortlink).toBe('https://status.madfam.io/incidents/inc-001')
  })
})

// ---- Scheduled maintenance shape ----

describe('buildSummary scheduled_maintenances', () => {
  const summary = buildFixture()
  const m = summary.scheduled_maintenances[0]

  it('marks future windows as scheduled', () => {
    expect(m.status).toBe('scheduled')
    expect(m.scheduled_for).toBe('2026-04-17T13:00:00.000Z')
    expect(m.scheduled_until).toBe('2026-04-17T14:00:00.000Z')
  })

  it('links affected components by id', () => {
    expect(m.components).toHaveLength(2)
    const names = m.components.map(c => c.name).sort()
    expect(names).toEqual(['Dhanam API', 'Karafiel API'])
  })

  it('transitions to in_progress when now is inside the window', () => {
    const midWindow = new Date('2026-04-17T13:30:00.000Z')
    const s = buildSummary({
      pageName: 'MADFAM',
      pageUrl: 'https://status.madfam.io',
      services: SERVICES,
      incidents: [],
      scheduledMaintenances: [MAINTENANCE],
      now: midWindow,
    })
    expect(s.scheduled_maintenances[0].status).toBe('in_progress')
  })

  it('transitions to completed after the window', () => {
    const afterWindow = new Date('2026-04-17T14:30:00.000Z')
    const s = buildSummary({
      pageName: 'MADFAM',
      pageUrl: 'https://status.madfam.io',
      services: SERVICES,
      incidents: [],
      scheduledMaintenances: [MAINTENANCE],
      now: afterWindow,
    })
    expect(s.scheduled_maintenances[0].status).toBe('completed')
    expect(s.scheduled_maintenances[0].resolved_at).toBe('2026-04-17T14:00:00.000Z')
  })
})

// ---- Snapshot of full shape against fixture ----

describe('buildSummary snapshot', () => {
  it('matches the canonical shape (keys only — values are deterministic)', () => {
    const summary = buildFixture()

    // Drive a shape-only snapshot to avoid brittleness on hash values.
    const shape = {
      topLevelKeys: Object.keys(summary).sort(),
      pageKeys: Object.keys(summary.page).sort(),
      statusKeys: Object.keys(summary.status).sort(),
      componentKeys: [...new Set(summary.components.flatMap(c => Object.keys(c)))].sort(),
      incidentKeys: [...new Set(summary.incidents.flatMap(i => Object.keys(i)))].sort(),
      incidentUpdateKeys: [
        ...new Set(
          summary.incidents.flatMap(i => i.incident_updates.flatMap(u => Object.keys(u))),
        ),
      ].sort(),
      maintenanceKeys: [
        ...new Set(summary.scheduled_maintenances.flatMap(m => Object.keys(m))),
      ].sort(),
    }

    expect(shape).toMatchSnapshot()
  })
})
