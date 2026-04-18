/**
 * Statuspage-compatible `/api/v2/summary.json` shim (RFC 0002 S2)
 *
 * Re-shapes our internal health check + incident data into the canonical
 * Atlassian Statuspage schema so downstream tools (Better Uptime, Datadog,
 * Slackbots) that auto-detect this shape work against us with zero
 * additional integration work.
 *
 * This is a READ-ONLY projection — no new data model, no persistence.
 *
 * Mapping reference:
 *   Our ServiceStatus          -> Statuspage component status
 *   ---------------------------------------------------------
 *   operational                -> operational
 *   degraded                   -> degraded_performance
 *   maintenance                -> under_maintenance
 *   outage                     -> major_outage
 *   unknown                    -> operational        (optimistic — unknown
 *                                                     typically means probe hasn't run yet)
 *
 *   Our worst ServiceStatus    -> Statuspage page-level indicator
 *   ---------------------------------------------------------
 *   operational                -> none
 *   degraded                   -> minor
 *   maintenance                -> maintenance
 *   outage                     -> critical
 *   unknown                    -> none
 *
 *   Our IncidentSeverity       -> Statuspage impact
 *   ---------------------------------------------------------
 *   minor                      -> minor
 *   major                      -> major
 *   critical                   -> critical
 *
 *   Our IncidentStatus         -> Statuspage incident status
 *   ---------------------------------------------------------
 *   investigating              -> investigating
 *   identified                 -> identified
 *   monitoring                 -> monitoring
 *   resolved                   -> resolved
 */

import { createHash } from 'crypto'
import type {
  HealthCheckResult,
  Incident,
  ScheduledMaintenance,
  ServiceStatus,
} from './types'

// -- Statuspage schema --

export type StatuspageComponentStatus =
  | 'operational'
  | 'degraded_performance'
  | 'partial_outage'
  | 'major_outage'
  | 'under_maintenance'

export type StatuspageIndicator =
  | 'none'
  | 'minor'
  | 'major'
  | 'critical'
  | 'maintenance'

export type StatuspageIncidentStatus =
  | 'investigating'
  | 'identified'
  | 'monitoring'
  | 'resolved'
  | 'postmortem'

export type StatuspageImpact = 'none' | 'minor' | 'major' | 'critical'

export type StatuspageMaintenanceStatus =
  | 'scheduled'
  | 'in_progress'
  | 'verifying'
  | 'completed'

export interface StatuspagePage {
  id: string
  name: string
  url: string
  time_zone: string
  updated_at: string
}

export interface StatuspageStatus {
  indicator: StatuspageIndicator
  description: string
}

export interface StatuspageComponent {
  id: string
  name: string
  status: StatuspageComponentStatus
  created_at: string
  updated_at: string
  position: number
  description: string | null
  showcase: boolean
  start_date: string | null
  group_id: string | null
  page_id: string
  group: boolean
  only_show_if_degraded: boolean
}

export interface StatuspageComponentGroup extends StatuspageComponent {
  group: true
  components: string[] // FK list of component ids
}

export interface StatuspageIncidentUpdate {
  id: string
  status: StatuspageIncidentStatus
  body: string
  incident_id: string
  created_at: string
  updated_at: string
  display_at: string
  affected_components: Array<{ code: string; name: string; old_status?: string; new_status?: string }>
  deliver_notifications: boolean
  custom_tweet: string | null
  tweet_id: string | null
}

export interface StatuspageIncident {
  id: string
  name: string
  status: StatuspageIncidentStatus
  created_at: string
  updated_at: string
  monitoring_at: string | null
  resolved_at: string | null
  impact: StatuspageImpact
  shortlink: string
  started_at: string
  page_id: string
  incident_updates: StatuspageIncidentUpdate[]
  components: Array<{ id: string; name: string }>
}

export interface StatuspageScheduledMaintenance {
  id: string
  name: string
  status: StatuspageMaintenanceStatus
  created_at: string
  updated_at: string
  monitoring_at: string | null
  resolved_at: string | null
  impact: StatuspageImpact
  shortlink: string
  started_at: string | null
  page_id: string
  incident_updates: StatuspageIncidentUpdate[]
  components: Array<{ id: string; name: string }>
  scheduled_for: string
  scheduled_until: string
}

export interface StatuspageSummary {
  page: StatuspagePage
  status: StatuspageStatus
  components: StatuspageComponent[]
  incidents: StatuspageIncident[]
  scheduled_maintenances: StatuspageScheduledMaintenance[]
}

// -- Helpers --

/**
 * Deterministic stable id derived from a name. Statuspage's real IDs are
 * opaque, we generate stable hashes so downstream consumers can cache by id.
 */
export function stableId(input: string): string {
  return createHash('sha1').update(input).digest('hex').slice(0, 12)
}

export function mapServiceStatusToComponent(
  status: ServiceStatus,
): StatuspageComponentStatus {
  switch (status) {
    case 'operational':
      return 'operational'
    case 'degraded':
      return 'degraded_performance'
    case 'maintenance':
      return 'under_maintenance'
    case 'outage':
      return 'major_outage'
    case 'unknown':
      // Optimistic default — unknown typically means the probe hasn't run yet,
      // not that the service is down. Statuspage has no "unknown" enum value.
      return 'operational'
  }
}

export function mapWorstStatusToIndicator(
  status: ServiceStatus,
): { indicator: StatuspageIndicator; description: string } {
  switch (status) {
    case 'operational':
      return { indicator: 'none', description: 'All Systems Operational' }
    case 'degraded':
      return { indicator: 'minor', description: 'Partially Degraded Service' }
    case 'maintenance':
      return { indicator: 'maintenance', description: 'Scheduled Maintenance In Progress' }
    case 'outage':
      return { indicator: 'critical', description: 'Major Service Outage' }
    case 'unknown':
      return { indicator: 'none', description: 'System Status Unknown' }
  }
}

export function mapIncidentSeverityToImpact(
  severity: Incident['severity'],
): StatuspageImpact {
  switch (severity) {
    case 'minor':
      return 'minor'
    case 'major':
      return 'major'
    case 'critical':
      return 'critical'
  }
}

/**
 * Map our incident lifecycle to Statuspage's. Our model supports the same
 * four literal states (investigating / identified / monitoring / resolved)
 * so this is an identity mapping today.
 */
export function mapIncidentStatus(
  status: Incident['status'],
): StatuspageIncidentStatus {
  return status
}

function worstStatus(services: HealthCheckResult[]): ServiceStatus {
  if (services.length === 0) return 'unknown'
  const priority: Record<ServiceStatus, number> = {
    outage: 0,
    degraded: 1,
    maintenance: 2,
    unknown: 3,
    operational: 4,
  }
  let worst: ServiceStatus = 'operational'
  for (const s of services) {
    if (priority[s.status] < priority[worst]) worst = s.status
  }
  return worst
}

// -- Public builder --

export interface BuildSummaryInput {
  pageName: string
  pageUrl: string
  pageId?: string
  timeZone?: string
  services: HealthCheckResult[]
  incidents: Incident[]
  scheduledMaintenances?: ScheduledMaintenance[]
  now?: Date
}

/**
 * Build a Statuspage-compatible `/api/v2/summary.json` payload from our
 * internal health check + incident data.
 *
 * Component groups come from:
 *   1. `service.family` when present (RFC 0002 S1 product families)
 *   2. `service.group` otherwise
 *
 * When both are present, family wraps group. Currently Statuspage's
 * schema only supports a single level of grouping, so we flatten to:
 *   components_group = family || group
 * to keep the v2 shim strictly canonical.
 */
export function buildSummary(input: BuildSummaryInput): StatuspageSummary {
  const now = input.now ?? new Date()
  const pageId = input.pageId ?? stableId(input.pageUrl)
  const timeZone = input.timeZone ?? 'Etc/UTC'

  // ---- Components (one per service) ----
  // Flat grouping key: prefer family, fall back to group.
  type GroupKey = string
  const groupMap = new Map<GroupKey, string[]>() // group_id -> component ids
  const groupNames = new Map<GroupKey, string>() // group_id -> display name

  // Sort services deterministically by (family || group, name) so positions
  // and ids are stable across calls for testing and caching.
  const sortedServices = [...input.services].sort((a, b) => {
    const keyA = (a.family ?? a.group) + '\u0000' + a.service
    const keyB = (b.family ?? b.group) + '\u0000' + b.service
    return keyA.localeCompare(keyB)
  })

  const components: StatuspageComponent[] = sortedServices.map((svc, index) => {
    const groupName = svc.family ?? svc.group
    const groupId = stableId(`group:${groupName}`)
    groupNames.set(groupId, groupName)

    const componentId = stableId(`component:${svc.service}:${svc.url}`)
    const list = groupMap.get(groupId) ?? []
    list.push(componentId)
    groupMap.set(groupId, list)

    return {
      id: componentId,
      name: svc.service,
      status: mapServiceStatusToComponent(svc.status),
      created_at: svc.lastChecked,
      updated_at: svc.lastChecked,
      position: index + 1,
      description: svc.description ?? null,
      showcase: false,
      start_date: null,
      group_id: groupId,
      page_id: pageId,
      group: false,
      only_show_if_degraded: false,
    }
  })

  // ---- Component groups ----
  // Index services by the group_id their component resolves to, so we can
  // compute each group's worst status without re-hashing.
  const servicesByGroupId = new Map<string, HealthCheckResult[]>()
  for (const svc of input.services) {
    const groupName = svc.family ?? svc.group
    const groupId = stableId(`group:${groupName}`)
    const list = servicesByGroupId.get(groupId) ?? []
    list.push(svc)
    servicesByGroupId.set(groupId, list)
  }

  // Each group is represented as a "component" with `group: true` and a
  // `components` FK array. Mirrors Statuspage exactly.
  const groupComponents: StatuspageComponentGroup[] = [...groupMap.entries()]
    .sort((a, b) => (groupNames.get(a[0]) ?? '').localeCompare(groupNames.get(b[0]) ?? ''))
    .map(([groupId, componentIds], idx) => {
      const name = groupNames.get(groupId)!
      const members = servicesByGroupId.get(groupId) ?? []
      const groupStatus = mapServiceStatusToComponent(worstStatus(members))
      return {
        id: groupId,
        name,
        status: groupStatus,
        created_at: now.toISOString(),
        updated_at: now.toISOString(),
        position: idx + 1,
        description: null,
        showcase: false,
        start_date: null,
        group_id: null,
        page_id: pageId,
        group: true,
        only_show_if_degraded: false,
        components: componentIds,
      }
    })

  // ---- Overall page status ----
  const overallIndicator = mapWorstStatusToIndicator(worstStatus(input.services))

  // ---- Incidents ----
  const incidents: StatuspageIncident[] = input.incidents.map(inc => {
    // Build a name->componentId index so we can fill the `components` FK.
    const affectedComponents = inc.affectedServices
      .map(name => components.find(c => c.name === name))
      .filter((c): c is StatuspageComponent => c !== undefined)
      .map(c => ({ id: c.id, name: c.name }))

    const updates: StatuspageIncidentUpdate[] = inc.updates.map(u => ({
      id: u.id,
      status: (u.status ?? inc.status) as StatuspageIncidentStatus,
      body: u.message,
      incident_id: inc.id,
      created_at: u.createdAt,
      updated_at: u.createdAt,
      display_at: u.createdAt,
      affected_components: affectedComponents.map(c => ({ code: c.id, name: c.name })),
      deliver_notifications: false,
      custom_tweet: null,
      tweet_id: null,
    }))

    const shortlink = `${input.pageUrl.replace(/\/$/, '')}/incidents/${inc.id}`

    return {
      id: inc.id,
      name: inc.title,
      status: mapIncidentStatus(inc.status),
      created_at: inc.createdAt,
      updated_at: updates.length > 0 ? updates[updates.length - 1].created_at : inc.createdAt,
      monitoring_at: inc.status === 'monitoring' ? inc.createdAt : null,
      resolved_at: inc.resolvedAt ?? null,
      impact: mapIncidentSeverityToImpact(inc.severity),
      shortlink,
      started_at: inc.createdAt,
      page_id: pageId,
      incident_updates: updates,
      components: affectedComponents,
    }
  })

  // ---- Scheduled maintenances ----
  const maintenances: StatuspageScheduledMaintenance[] = (input.scheduledMaintenances ?? []).map(m => {
    const nowMs = now.getTime()
    const startMs = new Date(m.scheduledStart).getTime()
    const endMs = new Date(m.scheduledEnd).getTime()
    let status: StatuspageMaintenanceStatus = 'scheduled'
    if (nowMs >= endMs) status = 'completed'
    else if (nowMs >= startMs) status = 'in_progress'

    const affectedComponents = m.affectedServices
      .map(name => components.find(c => c.name === name))
      .filter((c): c is StatuspageComponent => c !== undefined)
      .map(c => ({ id: c.id, name: c.name }))

    return {
      id: m.id,
      name: m.title,
      status,
      created_at: m.createdAt,
      updated_at: m.createdAt,
      monitoring_at: null,
      resolved_at: status === 'completed' ? m.scheduledEnd : null,
      impact: 'maintenance' as unknown as StatuspageImpact, // Statuspage uses 'maintenance' for windows
      shortlink: `${input.pageUrl.replace(/\/$/, '')}/maintenances/${m.id}`,
      started_at: status === 'scheduled' ? null : m.scheduledStart,
      page_id: pageId,
      incident_updates: [],
      components: affectedComponents,
      scheduled_for: m.scheduledStart,
      scheduled_until: m.scheduledEnd,
    }
  })

  return {
    page: {
      id: pageId,
      name: input.pageName,
      url: input.pageUrl,
      time_zone: timeZone,
      updated_at: now.toISOString(),
    },
    status: overallIndicator,
    components: [...groupComponents, ...components],
    incidents,
    scheduled_maintenances: maintenances,
  }
}
