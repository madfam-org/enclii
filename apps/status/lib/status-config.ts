import type { ServiceStatus, IncidentStatus, IncidentSeverity } from './types'
import {
  AlertCircle,
  AlertTriangle,
  CheckCircle,
  Clock,
  Eye,
  Search,
} from 'lucide-react'
import type { LucideIcon } from 'lucide-react'

/**
 * Response time status type
 */
export type ResponseTimeStatus = 'fast' | 'normal' | 'slow' | 'critical' | 'unknown'

/**
 * Shared status color classes for bar/dot rendering
 */
export const STATUS_COLORS: Record<ServiceStatus, string> = {
  operational: 'bg-status-operational',
  degraded: 'bg-status-degraded',
  outage: 'bg-status-outage',
  maintenance: 'bg-status-maintenance',
  unknown: 'bg-muted',
}

/**
 * Shared status labels
 */
export const STATUS_LABELS: Record<ServiceStatus, string> = {
  operational: 'Operational',
  degraded: 'Degraded',
  outage: 'Major Outage',
  maintenance: 'Maintenance',
  unknown: 'Unknown',
}

/**
 * Full status config for badges (dot, background, text classes)
 */
export const STATUS_CONFIG: Record<ServiceStatus, {
  label: string
  dotClass: string
  bgClass: string
  textClass: string
}> = {
  operational: {
    label: 'Operational',
    dotClass: 'bg-status-operational',
    bgClass: 'bg-status-operational-muted',
    textClass: 'text-status-operational',
  },
  degraded: {
    label: 'Degraded',
    dotClass: 'bg-status-degraded',
    bgClass: 'bg-status-degraded-muted',
    textClass: 'text-status-degraded',
  },
  outage: {
    label: 'Major Outage',
    dotClass: 'bg-status-outage',
    bgClass: 'bg-status-outage-muted',
    textClass: 'text-status-outage',
  },
  maintenance: {
    label: 'Maintenance',
    dotClass: 'bg-status-maintenance',
    bgClass: 'bg-status-maintenance-muted',
    textClass: 'text-status-maintenance',
  },
  unknown: {
    label: 'Unknown',
    dotClass: 'bg-muted-foreground',
    bgClass: 'bg-muted',
    textClass: 'text-muted-foreground',
  },
}

/**
 * Generic group-by utility
 */
export function groupByKey<T>(items: T[], keyFn: (item: T) => string): Map<string, T[]> {
  const groups = new Map<string, T[]>()
  for (const item of items) {
    const key = keyFn(item)
    const list = groups.get(key) ?? []
    list.push(item)
    groups.set(key, list)
  }
  return groups
}

/**
 * Status priority for determining worst status (lower = worse)
 */
export const STATUS_PRIORITY: Record<ServiceStatus, number> = {
  outage: 0,
  degraded: 1,
  maintenance: 2,
  unknown: 3,
  operational: 4,
}

/**
 * Response time color classes (text)
 */
export const RESPONSE_TIME_COLORS: Record<ResponseTimeStatus, string> = {
  fast: 'text-status-operational',
  normal: 'text-status-degraded',
  slow: 'text-status-degraded',
  critical: 'text-status-degraded',
  unknown: 'text-muted-foreground',
}

/**
 * Response time labels
 */
export const RESPONSE_TIME_LABELS: Record<ResponseTimeStatus, string> = {
  fast: 'Fast',
  normal: 'Normal',
  slow: 'Slow',
  critical: 'Very Slow',
  unknown: 'Unknown',
}

/**
 * Response time bar background colors
 */
export const RESPONSE_TIME_BAR_COLORS: Record<ResponseTimeStatus, string> = {
  fast: 'bg-status-operational',
  normal: 'bg-status-degraded',
  slow: 'bg-status-degraded',
  critical: 'bg-status-degraded',
  unknown: 'bg-muted',
}

/**
 * Incident status config (icon, label, colors)
 */
export const INCIDENT_STATUS_CONFIG: Record<IncidentStatus, {
  icon: LucideIcon
  label: string
  color: string
  bgColor: string
}> = {
  investigating: {
    icon: Search,
    label: 'Investigating',
    color: 'text-status-outage',
    bgColor: 'bg-status-outage-muted',
  },
  identified: {
    icon: Eye,
    label: 'Identified',
    color: 'text-status-degraded',
    bgColor: 'bg-status-degraded-muted',
  },
  monitoring: {
    icon: Clock,
    label: 'Monitoring',
    color: 'text-status-maintenance',
    bgColor: 'bg-status-maintenance-muted',
  },
  resolved: {
    icon: CheckCircle,
    label: 'Resolved',
    color: 'text-status-operational',
    bgColor: 'bg-status-operational-muted',
  },
}

/**
 * Incident severity config (icon, label, color)
 */
export const INCIDENT_SEVERITY_CONFIG: Record<IncidentSeverity, {
  icon: LucideIcon
  label: string
  color: string
}> = {
  minor: {
    icon: AlertCircle,
    label: 'Minor',
    color: 'text-status-degraded',
  },
  major: {
    icon: AlertTriangle,
    label: 'Major',
    color: 'text-status-outage',
  },
  critical: {
    icon: AlertTriangle,
    label: 'Critical',
    color: 'text-status-outage',
  },
}
