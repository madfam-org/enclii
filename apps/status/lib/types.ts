/**
 * Service status types
 */
export type ServiceStatus = 'operational' | 'degraded' | 'outage' | 'maintenance' | 'unknown'

/**
 * Incident status types
 */
export type IncidentStatus = 'investigating' | 'identified' | 'monitoring' | 'resolved'

/**
 * Incident severity levels
 */
export type IncidentSeverity = 'minor' | 'major' | 'critical'

/**
 * Service configuration from environment
 */
export interface ServiceConfig {
  name: string
  url: string          // Health check URL (used for monitoring)
  href?: string        // User-facing URL (used for links). Falls back to url.
  group: string
  description?: string
}

/**
 * Real-time health check result
 */
export interface HealthCheckResult {
  service: string
  url: string
  href?: string        // User-facing URL for display links
  group: string
  description?: string
  status: ServiceStatus
  responseTime: number | null
  lastChecked: string
  statusCode?: number
  error?: string
}

/**
 * Historical uptime data from Prometheus
 */
export interface UptimeData {
  service: string
  uptime24h: number | null
  uptime7d: number | null
  uptime30d: number | null
  uptime90d: number | null
  dailyHistory: DayStatus[]
}

/**
 * Daily status for uptime bar visualization
 */
export interface DayStatus {
  date: string
  status: ServiceStatus
  uptimePercent: number
}

/**
 * Aggregated status response
 */
export interface StatusResponse {
  overall: ServiceStatus
  lastUpdated: string
  services: HealthCheckResult[]
  uptimeData?: Record<string, UptimeData>
}

/**
 * Incident record
 */
export interface Incident {
  id: string
  title: string
  status: IncidentStatus
  severity: IncidentSeverity
  affectedServices: string[]
  createdAt: string
  resolvedAt?: string
  updates: IncidentUpdate[]
}

/**
 * Incident update
 */
export interface IncidentUpdate {
  id: string
  incidentId: string
  message: string
  status?: IncidentStatus
  createdAt: string
}

/**
 * Scheduled maintenance
 */
export interface ScheduledMaintenance {
  id: string
  title: string
  description?: string
  affectedServices: string[]
  scheduledStart: string
  scheduledEnd: string
  createdAt: string
}

/**
 * Site configuration
 */
export interface SiteConfig {
  name: string
  url: string
  services: ServiceConfig[]
}

/**
 * Raw status check record (stored in status_checks table)
 */
export interface StatusCheckRecord {
  id: number
  service: string
  url: string
  groupName: string
  status: ServiceStatus
  responseMs: number | null
  statusCode: number | null
  error: string | null
  checkedAt: string
}

/**
 * A single time window in the 24h timeline
 */
export interface TimelineSlot {
  start: string           // ISO timestamp of window start
  end: string             // ISO timestamp of window end
  status: ServiceStatus   // worst status in window
  checks: number          // number of checks in window
  avgResponseMs: number | null
}

/**
 * Timeline data for one service
 */
export interface ServiceTimeline {
  service: string
  group: string
  url: string
  href?: string           // user-facing URL for display links
  slots: TimelineSlot[]
  uptime24h: number       // percentage
}

/**
 * Full timeline API response
 */
export interface TimelineResponse {
  services: ServiceTimeline[]
  from: string
  to: string
  windowMinutes: number
}

/**
 * Response time thresholds for visual indicators
 */
export const RESPONSE_TIME_THRESHOLDS = {
  fast: 200,    // < 200ms = green
  normal: 500,  // 200-500ms = yellow
  slow: 1000,   // 500-1000ms = orange
  // > 1000ms = red
} as const

/**
 * Get status from response time
 */
export function getResponseTimeStatus(ms: number | null): 'fast' | 'normal' | 'slow' | 'critical' | 'unknown' {
  if (ms === null) return 'unknown'
  if (ms < RESPONSE_TIME_THRESHOLDS.fast) return 'fast'
  if (ms < RESPONSE_TIME_THRESHOLDS.normal) return 'normal'
  if (ms < RESPONSE_TIME_THRESHOLDS.slow) return 'slow'
  return 'critical'
}

/**
 * Calculate overall status from service statuses using STATUS_PRIORITY.
 * Returns the worst (lowest priority) status found.
 */
export function calculateOverallStatus(services: HealthCheckResult[]): ServiceStatus {
  if (services.length === 0) return 'unknown'

  // Inline priority to avoid circular dependency with status-config
  const priority: Record<ServiceStatus, number> = {
    outage: 0, degraded: 1, maintenance: 2, unknown: 3, operational: 4,
  }

  let worst: ServiceStatus = 'operational'
  for (const s of services) {
    if (priority[s.status] < priority[worst]) {
      worst = s.status
    }
  }
  return worst
}
